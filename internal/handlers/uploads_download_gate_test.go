package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/dbtest"
	"github.com/WithAutonomi/indelible/internal/handlers"
	"github.com/WithAutonomi/indelible/internal/services"
)

// TestDownloadUpload_AdmissionGate covers V2-809: downloads beyond
// max_concurrent_downloads wait up to download_queue_wait_secs for a slot and
// are then rejected 503 downloads_saturated with a Retry-After hint; the ETag
// 304 path bypasses the gate entirely (no antd fetch happens); and a freed
// slot restores normal admission.
func TestDownloadUpload_AdmissionGate(t *testing.T) {
	const fileContent = "bytes served under admission control"

	// Fake antd: when blockDownloads is set, a /v1/files/get call parks until
	// releaseAntd closes — pinning its download in the "in flight" state so
	// the test can saturate the gate deterministically.
	var blockDownloads atomic.Bool
	var antdHits atomic.Int32
	antdEntered := make(chan struct{}, 1)
	releaseAntd := make(chan struct{})
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/files/get" {
			http.Error(w, "{}", http.StatusNotFound)
			return
		}
		antdHits.Add(1)
		if blockDownloads.Load() {
			antdEntered <- struct{}{}
			<-releaseAntd
		}
		var body struct {
			DestPath string `json:"dest_path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.DestPath == "" {
			t.Errorf("antd request missing dest_path (err=%v)", err)
			http.Error(w, "{}", http.StatusBadRequest)
			return
		}
		if err := os.WriteFile(body.DestPath, []byte(fileContent), 0600); err != nil {
			t.Errorf("fake antd write dest_path: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer fake.Close()

	cfg := &config.Config{
		Port:                8080,
		AntdURL:             fake.URL,
		DataDir:             t.TempDir(),
		JWTSecret:           "test-secret-for-jwt-signing-1234567890",
		WalletEncryptionKey: "0000000000000000000000000000000000000000000000000000000000000000",
		AdminEmail:          seedAdminEmail,
		AdminPassword:       seedAdminPassword,
	}
	db := dbtest.OpenDB(t)
	if _, err := services.SeedAdmin(db, cfg); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	// One slot, fail-fast queue: the second concurrent download must be
	// rejected immediately rather than waiting for the first to finish.
	settingsSvc := services.NewSettingsService(db)
	if err := settingsSvc.SetInternal("max_concurrent_downloads", "1"); err != nil {
		t.Fatalf("set max_concurrent_downloads: %v", err)
	}
	if err := settingsSvc.SetInternal("download_queue_wait_secs", "0"); err != nil {
		t.Fatalf("set download_queue_wait_secs: %v", err)
	}
	router := handlers.NewRouter(cfg, db, nil, nil)
	adminToken := registerAndGetToken(t, router, seedAdminEmail, seedAdminPassword, "Admin", "User")
	createTestWallet(t, router, adminToken)

	uuid := uploadAndGetUUID(t, router, adminToken, "gated.txt")
	if _, err := db.Exec("UPDATE uploads SET status='completed', visibility='private', data_map='deadbeef', datamap_address=NULL WHERE uuid = ?", uuid); err != nil {
		t.Fatalf("promote upload: %v", err)
	}

	download := func(ifNoneMatch string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/v2/uploads/"+uuid+"/download", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		if ifNoneMatch != "" {
			req.Header.Set("If-None-Match", ifNoneMatch)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	// Below the cap: a download succeeds normally; learn the upload's ETag.
	w := download("")
	if w.Code != http.StatusOK {
		t.Fatalf("unsaturated download: got %d, want 200; body: %s", w.Code, w.Body.String())
	}
	etag := w.Result().Header.Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag on download response")
	}

	// Saturate: park a download inside the fake antd, holding the only slot.
	blockDownloads.Store(true)
	parked := make(chan *httptest.ResponseRecorder, 1)
	go func() { parked <- download("") }()
	select {
	case <-antdEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("parked download never reached antd")
	}

	// A second download is rejected: 503, machine-readable code, retry hint.
	w = download("")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("saturated download: got %d, want 503; body: %s", w.Code, w.Body.String())
	}
	if got := w.Result().Header.Get("Retry-After"); got != "30" {
		t.Errorf("Retry-After = %q, want %q", got, "30")
	}
	var errBody struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &errBody); err != nil || errBody.Code != "downloads_saturated" {
		t.Errorf("error code = %q (err=%v), want downloads_saturated; body: %s", errBody.Code, err, w.Body.String())
	}

	// The conditional path bypasses the gate: a 304 needs no antd fetch and
	// no temp file, so saturation must not block it.
	hitsBefore := antdHits.Load()
	w = download(etag)
	if w.Code != http.StatusNotModified {
		t.Fatalf("conditional download under saturation: got %d, want 304; body: %s", w.Code, w.Body.String())
	}
	if antdHits.Load() != hitsBefore {
		t.Error("304 path hit antd; the fetch must be skipped")
	}

	// Free the slot: the parked download completes and admission resumes.
	blockDownloads.Store(false)
	close(releaseAntd)
	select {
	case pw := <-parked:
		if pw.Code != http.StatusOK {
			t.Fatalf("parked download: got %d, want 200; body: %s", pw.Code, pw.Body.String())
		}
		if pw.Body.String() != fileContent {
			t.Errorf("parked download body = %q, want %q", pw.Body.String(), fileContent)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("parked download never completed after release")
	}
	w = download("")
	if w.Code != http.StatusOK {
		t.Fatalf("download after drain: got %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

// TestDownloadUpload_QueuedDownloadCompletes covers the queued-then-served
// path end to end: with a non-zero download_queue_wait_secs, a request that
// arrives while the only slot is held parks in the gate's queue (no 503) and
// completes normally once the slot frees. The saturation test above only
// exercises fail-fast rejection (queue_wait_secs=0).
func TestDownloadUpload_QueuedDownloadCompletes(t *testing.T) {
	const fileContent = "bytes served after queueing for a slot"

	var blockDownloads atomic.Bool
	antdEntered := make(chan struct{}, 1)
	releaseAntd := make(chan struct{})
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/files/get" {
			http.Error(w, "{}", http.StatusNotFound)
			return
		}
		if blockDownloads.Load() {
			antdEntered <- struct{}{}
			<-releaseAntd
		}
		var body struct {
			DestPath string `json:"dest_path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.DestPath == "" {
			t.Errorf("antd request missing dest_path (err=%v)", err)
			http.Error(w, "{}", http.StatusBadRequest)
			return
		}
		if err := os.WriteFile(body.DestPath, []byte(fileContent), 0600); err != nil {
			t.Errorf("fake antd write dest_path: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer fake.Close()

	cfg := &config.Config{
		Port:                8080,
		AntdURL:             fake.URL,
		DataDir:             t.TempDir(),
		JWTSecret:           "test-secret-for-jwt-signing-1234567890",
		WalletEncryptionKey: "0000000000000000000000000000000000000000000000000000000000000000",
		AdminEmail:          seedAdminEmail,
		AdminPassword:       seedAdminPassword,
	}
	db := dbtest.OpenDB(t)
	if _, err := services.SeedAdmin(db, cfg); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	// One slot, generous queue window: the second download must wait for the
	// slot rather than being rejected.
	settingsSvc := services.NewSettingsService(db)
	if err := settingsSvc.SetInternal("max_concurrent_downloads", "1"); err != nil {
		t.Fatalf("set max_concurrent_downloads: %v", err)
	}
	if err := settingsSvc.SetInternal("download_queue_wait_secs", "10"); err != nil {
		t.Fatalf("set download_queue_wait_secs: %v", err)
	}
	router := handlers.NewRouter(cfg, db, nil, nil)
	adminToken := registerAndGetToken(t, router, seedAdminEmail, seedAdminPassword, "Admin", "User")
	createTestWallet(t, router, adminToken)

	uuid := uploadAndGetUUID(t, router, adminToken, "queued.txt")
	if _, err := db.Exec("UPDATE uploads SET status='completed', visibility='private', data_map='deadbeef', datamap_address=NULL WHERE uuid = ?", uuid); err != nil {
		t.Fatalf("promote upload: %v", err)
	}

	download := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/v2/uploads/"+uuid+"/download", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	// Hold the only slot: a download parked inside the fake antd.
	blockDownloads.Store(true)
	holder := make(chan *httptest.ResponseRecorder, 1)
	go func() { holder <- download() }()
	select {
	case <-antdEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("slot-holding download never reached antd")
	}

	// The second download queues in the gate instead of failing.
	queued := make(chan *httptest.ResponseRecorder, 1)
	go func() { queued <- download() }()
	select {
	case qw := <-queued:
		t.Fatalf("queued download returned %d before the slot freed; body: %s", qw.Code, qw.Body.String())
	case <-time.After(200 * time.Millisecond):
	}

	// Free the slot; both downloads complete with the file bytes.
	blockDownloads.Store(false)
	close(releaseAntd)
	for name, ch := range map[string]chan *httptest.ResponseRecorder{"holder": holder, "queued": queued} {
		select {
		case w := <-ch:
			if w.Code != http.StatusOK {
				t.Fatalf("%s download: got %d, want 200; body: %s", name, w.Code, w.Body.String())
			}
			if w.Body.String() != fileContent {
				t.Errorf("%s download body = %q, want %q", name, w.Body.String(), fileContent)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s download never completed after release", name)
		}
	}
}
