package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/dbtest"
	"github.com/WithAutonomi/indelible/internal/handlers"
	"github.com/WithAutonomi/indelible/internal/services"
)

// cacheFakeAntd is a fake antd daemon for download-cache tests: it serves
// both the public and private fetch endpoints, counts hits per endpoint, and
// can park private fetches so a test can pin the download gate.
type cacheFakeAntd struct {
	srv            *httptest.Server
	publicHits     atomic.Int32
	privateHits    atomic.Int32
	blockPrivate   atomic.Bool
	privateEntered chan struct{}
	releasePrivate chan struct{}
	blockPublic    atomic.Bool
	publicEntered  chan struct{}
	releasePublic  chan struct{}
}

func newCacheFakeAntd(t *testing.T, content string) *cacheFakeAntd {
	t.Helper()
	f := &cacheFakeAntd{
		privateEntered: make(chan struct{}, 1),
		releasePrivate: make(chan struct{}),
		publicEntered:  make(chan struct{}, 1),
		releasePublic:  make(chan struct{}),
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/files/public/get":
			f.publicHits.Add(1)
			if f.blockPublic.Load() {
				f.publicEntered <- struct{}{}
				<-f.releasePublic
			}
		case "/v1/files/get":
			f.privateHits.Add(1)
			if f.blockPrivate.Load() {
				f.privateEntered <- struct{}{}
				<-f.releasePrivate
			}
		default:
			http.Error(w, "{}", http.StatusNotFound)
			return
		}
		var body struct {
			DestPath string `json:"dest_path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.DestPath == "" {
			t.Errorf("antd request missing dest_path (err=%v)", err)
			http.Error(w, "{}", http.StatusBadRequest)
			return
		}
		if err := os.WriteFile(body.DestPath, []byte(content), 0600); err != nil {
			t.Errorf("fake antd write dest_path: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// newCacheTestEnv builds a router against the fake antd with the given
// runtime settings applied before the settings cache warms.
func newCacheTestEnv(t *testing.T, antdURL string, settings map[string]string) (http.Handler, string, *config.Config, *database.DB) {
	t.Helper()
	cfg := &config.Config{
		Port:                8080,
		AntdURL:             antdURL,
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
	settingsSvc := services.NewSettingsService(db)
	for k, v := range settings {
		if err := settingsSvc.SetInternal(k, v); err != nil {
			t.Fatalf("set %s: %v", k, err)
		}
	}
	router := handlers.NewRouter(cfg, db, nil)
	adminToken := registerAndGetToken(t, router, seedAdminEmail, seedAdminPassword, "Admin", "User")
	createTestWallet(t, router, adminToken)
	return router, adminToken, cfg, db
}

// makeUpload creates a completed upload row: public rows get a network
// address (FileGetPublic path), private rows a local DataMap (FileGet path).
func makeUpload(t *testing.T, router http.Handler, db *database.DB, token, filename, visibility, id string) string {
	t.Helper()
	uuid := uploadAndGetUUID(t, router, token, filename)
	var err error
	if visibility == "public" {
		_, err = db.Exec("UPDATE uploads SET status='completed', visibility='public', data_map=NULL, datamap_address=? WHERE uuid = ?", id, uuid)
	} else {
		_, err = db.Exec("UPDATE uploads SET status='completed', visibility='private', data_map=?, datamap_address=NULL WHERE uuid = ?", id, uuid)
	}
	if err != nil {
		t.Fatalf("promote upload: %v", err)
	}
	return uuid
}

func doDownload(router http.Handler, token, uuid, rangeHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/api/v2/uploads/"+uuid+"/download", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// With no byte budget configured the cache is off: every download fetches.
func TestDownloadUpload_CacheDisabledByDefault(t *testing.T) {
	fake := newCacheFakeAntd(t, "uncached bytes")
	router, token, _, db := newCacheTestEnv(t, fake.srv.URL, nil)
	uuid := makeUpload(t, router, db, token, "plain.txt", "public", "addr-disabled")

	for i := 0; i < 2; i++ {
		if w := doDownload(router, token, uuid, ""); w.Code != http.StatusOK {
			t.Fatalf("download %d: got %d; body: %s", i+1, w.Code, w.Body.String())
		}
	}
	if got := fake.publicHits.Load(); got != 2 {
		t.Fatalf("antd public fetches = %d, want 2 (cache must be off by default)", got)
	}
}

// The core V2-821 path: first public download promotes into the cache, the
// repeat is served from local disk (no antd fetch, Range preserved) and slips
// past a saturated download gate; private content is never cached.
func TestDownloadUpload_CacheHitServesLocallyAndBypassesGate(t *testing.T) {
	const content = "cache me once, serve me twice"
	fake := newCacheFakeAntd(t, content)
	router, token, cfg, db := newCacheTestEnv(t, fake.srv.URL, map[string]string{
		"download_cache_max_bytes": "1048576",
		"max_concurrent_downloads": "1",
		"download_queue_wait_secs": "0",
	})
	pubUUID := makeUpload(t, router, db, token, "hot-page.txt", "public", "addr-hot")
	privUUID := makeUpload(t, router, db, token, "secret.txt", "private", "deadbeef")

	// Miss → fetch → promote.
	w := doDownload(router, token, pubUUID, "")
	if w.Code != http.StatusOK || w.Body.String() != content {
		t.Fatalf("first download: %d %q", w.Code, w.Body.String())
	}
	etag := w.Result().Header.Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag on first download")
	}
	key := strings.Trim(etag, `"`)
	cachedFile := filepath.Join(cfg.DataDir, "cache", "objects", key[0:2], key[2:4], key)
	if b, err := os.ReadFile(cachedFile); err != nil || string(b) != content {
		t.Fatalf("promoted cache file %s = %q (err=%v), want %q", cachedFile, b, err, content)
	}

	// Hit: identical response, no further antd traffic, validators intact.
	w = doDownload(router, token, pubUUID, "")
	if w.Code != http.StatusOK || w.Body.String() != content {
		t.Fatalf("cached download: %d %q", w.Code, w.Body.String())
	}
	if got := w.Result().Header.Get("ETag"); got != etag {
		t.Errorf("cached ETag = %q, want %q", got, etag)
	}
	if cc := w.Result().Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("cached Cache-Control = %q, want immutable", cc)
	}
	if got := fake.publicHits.Load(); got != 1 {
		t.Fatalf("antd public fetches = %d, want 1 (repeat must hit the cache)", got)
	}

	// Range requests are honored from the cached file.
	w = doDownload(router, token, pubUUID, "bytes=0-4")
	if w.Code != http.StatusPartialContent || w.Body.String() != content[:5] {
		t.Fatalf("range from cache: %d %q, want 206 %q", w.Code, w.Body.String(), content[:5])
	}

	// Saturate the gate with a parked private download; the cached public
	// download must still be served — hits bypass the gate entirely.
	fake.blockPrivate.Store(true)
	parked := make(chan *httptest.ResponseRecorder, 1)
	go func() { parked <- doDownload(router, token, privUUID, "") }()
	select {
	case <-fake.privateEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("parked private download never reached antd")
	}
	w = doDownload(router, token, pubUUID, "")
	if w.Code != http.StatusOK {
		t.Fatalf("cached download under gate saturation: got %d, want 200 (hits must bypass the gate); body: %s", w.Code, w.Body.String())
	}
	fake.blockPrivate.Store(false)
	close(fake.releasePrivate)
	select {
	case pw := <-parked:
		if pw.Code != http.StatusOK {
			t.Fatalf("parked private download: got %d; body: %s", pw.Code, pw.Body.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("parked private download never completed")
	}

	// Private content is never cached: a repeat fetches again.
	if w := doDownload(router, token, privUUID, ""); w.Code != http.StatusOK {
		t.Fatalf("private repeat: %d", w.Code)
	}
	if got := fake.privateHits.Load(); got != 2 {
		t.Fatalf("antd private fetches = %d, want 2 (private must never be cached)", got)
	}
}

// Objects above download_cache_max_object_bytes stay in the temp-file regime.
func TestDownloadUpload_CacheSkipsOversizeObject(t *testing.T) {
	fake := newCacheFakeAntd(t, "definitely more than one byte")
	router, token, cfg, db := newCacheTestEnv(t, fake.srv.URL, map[string]string{
		"download_cache_max_bytes":        "1048576",
		"download_cache_max_object_bytes": "1",
	})
	uuid := makeUpload(t, router, db, token, "big.txt", "public", "addr-big")

	for i := 0; i < 2; i++ {
		if w := doDownload(router, token, uuid, ""); w.Code != http.StatusOK {
			t.Fatalf("download %d: got %d", i+1, w.Code)
		}
	}
	if got := fake.publicHits.Load(); got != 2 {
		t.Fatalf("antd public fetches = %d, want 2 (oversize object must not be cached)", got)
	}
	if entries, _ := os.ReadDir(filepath.Join(cfg.DataDir, "cache", "objects")); len(entries) != 0 {
		t.Fatalf("cache directory not empty after oversize skip: %v", entries)
	}
}

// With download_cache_min_uses raised, an object must miss that many times
// before it is promoted — the admission filter against one-hit wonders.
func TestDownloadUpload_CacheMinUses(t *testing.T) {
	const content = "hot enough on the second ask"
	fake := newCacheFakeAntd(t, content)
	router, token, _, db := newCacheTestEnv(t, fake.srv.URL, map[string]string{
		"download_cache_max_bytes": "1048576",
		"download_cache_min_uses":  "2",
	})
	uuid := makeUpload(t, router, db, token, "warming.txt", "public", "addr-warm")

	// First two requests fetch (miss 1 = not hot enough, miss 2 = promoted).
	for i := 0; i < 2; i++ {
		if w := doDownload(router, token, uuid, ""); w.Code != http.StatusOK {
			t.Fatalf("download %d: got %d", i+1, w.Code)
		}
	}
	if got := fake.publicHits.Load(); got != 2 {
		t.Fatalf("antd public fetches after warm-up = %d, want 2", got)
	}
	// Third request is served from the cache.
	if w := doDownload(router, token, uuid, ""); w.Code != http.StatusOK || w.Body.String() != content {
		t.Fatalf("cached download: %d %q", w.Code, w.Body.String())
	}
	if got := fake.publicHits.Load(); got != 2 {
		t.Fatalf("antd public fetches after promotion = %d, want 2 (third request must hit)", got)
	}
}

// Concurrent misses on one object are coalesced: only the leader fetches;
// the follower waits for the fill and serves the promoted bytes — one antd
// fetch total, not two.
func TestDownloadUpload_CacheCoalescesConcurrentFills(t *testing.T) {
	const content = "fetched once, served to two"
	fake := newCacheFakeAntd(t, content)
	router, token, _, db := newCacheTestEnv(t, fake.srv.URL, map[string]string{
		"download_cache_max_bytes": "1048576",
		"max_concurrent_downloads": "1",
		"download_queue_wait_secs": "10",
	})
	uuid := makeUpload(t, router, db, token, "stampede.txt", "public", "addr-stampede")

	// Leader parks inside the antd fetch, holding the fill (and the only
	// gate slot).
	fake.blockPublic.Store(true)
	leader := make(chan *httptest.ResponseRecorder, 1)
	go func() { leader <- doDownload(router, token, uuid, "") }()
	select {
	case <-fake.publicEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("leader download never reached antd")
	}

	// Follower arrives on the same object: it must join the fill, not fetch.
	follower := make(chan *httptest.ResponseRecorder, 1)
	go func() { follower <- doDownload(router, token, uuid, "") }()

	// Give the follower a moment to park, then let the leader finish.
	select {
	case fw := <-follower:
		t.Fatalf("follower returned %d before the fill completed", fw.Code)
	case <-time.After(200 * time.Millisecond):
	}
	fake.blockPublic.Store(false)
	close(fake.releasePublic)

	for name, ch := range map[string]chan *httptest.ResponseRecorder{"leader": leader, "follower": follower} {
		select {
		case w := <-ch:
			if w.Code != http.StatusOK || w.Body.String() != content {
				t.Fatalf("%s: %d %q", name, w.Code, w.Body.String())
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s never completed", name)
		}
	}
	if got := fake.publicHits.Load(); got != 1 {
		t.Fatalf("antd public fetches = %d, want 1 (concurrent misses must coalesce)", got)
	}
}

// Until the V2-823 sweeper lands the budget is enforced at admission: once
// full, the cache stops growing rather than evicting.
func TestDownloadUpload_CacheStopsAtBudget(t *testing.T) {
	const content = "thirteen bytes"
	fake := newCacheFakeAntd(t, content)
	router, token, _, db := newCacheTestEnv(t, fake.srv.URL, map[string]string{
		"download_cache_max_bytes": "4", // smaller than the object
	})
	uuid := makeUpload(t, router, db, token, "over-budget.txt", "public", "addr-budget")

	for i := 0; i < 2; i++ {
		if w := doDownload(router, token, uuid, ""); w.Code != http.StatusOK {
			t.Fatalf("download %d: got %d", i+1, w.Code)
		}
	}
	if got := fake.publicHits.Load(); got != 2 {
		t.Fatalf("antd public fetches = %d, want 2 (promotion must respect the byte budget)", got)
	}
}
