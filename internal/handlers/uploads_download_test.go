package handlers_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/dbtest"
	"github.com/WithAutonomi/indelible/internal/handlers"
	"github.com/WithAutonomi/indelible/internal/services"
)

// TestDownloadUpload_StreamsToDisk is the regression guard for V2-424: every
// download branch must use the streaming FileGet*/ primitives (daemon writes
// straight to dest_path) rather than DataGet, which buffered the whole file
// into indelible's heap — an OOM/DoS vector on large private downloads.
//
// A fake antd honours the streaming contract by writing the file to the
// daemon-provided dest_path (the shared-filesystem assumption the real
// deployment relies on), and records which endpoint each download hit:
//
//	private (DataMap or legacy DatamapAddress) -> POST /v1/files/get
//	public                                     -> POST /v1/files/public/get
//
// If a private branch ever regressed to DataGet (POST /v1/data/get) the fake
// would flag the unexpected path and fail.
func TestDownloadUpload_StreamsToDisk(t *testing.T) {
	const fileContent = "private bytes that must never be buffered whole in RAM"

	var lastPath string // antd endpoint hit by the most recent download
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath = r.URL.Path
		if r.URL.Path != "/v1/files/get" && r.URL.Path != "/v1/files/public/get" {
			t.Errorf("download hit unexpected antd endpoint %s (a private download must stream via /v1/files/get, not buffer via DataGet)", r.URL.Path)
			http.Error(w, "{}", http.StatusNotFound)
			return
		}
		var body struct {
			DestPath string `json:"dest_path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.DestPath == "" {
			t.Errorf("antd request missing dest_path (got err=%v): the daemon, not indelible, must own the file bytes", err)
			http.Error(w, "{}", http.StatusBadRequest)
			return
		}
		// Stand in for the daemon writing the file to disk at dest_path.
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
	router := handlers.NewRouter(cfg, db, nil)
	adminToken := registerAndGetToken(t, router, seedAdminEmail, seedAdminPassword, "Admin", "User")
	createTestWallet(t, router, adminToken)

	cases := []struct {
		name       string
		set        string // SQL SET clause promoting the queued upload to completed
		wantEndpnt string
	}{
		{
			name:       "external signer (local DataMap)",
			set:        "status='completed', visibility='private', data_map='deadbeef', datamap_address=NULL",
			wantEndpnt: "/v1/files/get",
		},
		{
			name:       "legacy private (DatamapAddress)",
			set:        "status='completed', visibility='private', data_map=NULL, datamap_address='0xabc123'",
			wantEndpnt: "/v1/files/get",
		},
		{
			name:       "public (DatamapAddress)",
			set:        "status='completed', visibility='public', data_map=NULL, datamap_address='0xpub456'",
			wantEndpnt: "/v1/files/public/get",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lastPath = ""
			uuid := uploadAndGetUUID(t, router, adminToken, "secret.txt")
			if _, err := db.Exec("UPDATE uploads SET "+tc.set+" WHERE uuid = ?", uuid); err != nil {
				t.Fatalf("promote upload: %v", err)
			}

			req := httptest.NewRequest("GET", "/api/v2/uploads/"+uuid+"/download", nil)
			req.Header.Set("Authorization", "Bearer "+adminToken)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("download: got %d, want 200; body: %s", w.Code, w.Body.String())
			}
			gotBody, _ := io.ReadAll(w.Result().Body)
			if string(gotBody) != fileContent {
				t.Errorf("download body = %q, want %q", string(gotBody), fileContent)
			}
			if lastPath != tc.wantEndpnt {
				t.Errorf("antd endpoint = %q, want %q", lastPath, tc.wantEndpnt)
			}

			// V2-516: a successful download carries strong cache validators for
			// the immutable, content-addressed bytes.
			etag := w.Result().Header.Get("ETag")
			if etag == "" {
				t.Error("missing ETag on download response")
			}
			if cc := w.Result().Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
				t.Errorf("Cache-Control = %q, want an immutable directive", cc)
			}

			// Conditional re-request with that ETag → 304, and antd is NOT hit
			// again: the bytes are immutable, so the fetch is skipped entirely.
			// This is the reader-fleet throughput win (V2-516).
			lastPath = ""
			req2 := httptest.NewRequest("GET", "/api/v2/uploads/"+uuid+"/download", nil)
			req2.Header.Set("Authorization", "Bearer "+adminToken)
			req2.Header.Set("If-None-Match", etag)
			w2 := httptest.NewRecorder()
			router.ServeHTTP(w2, req2)
			if w2.Code != http.StatusNotModified {
				t.Fatalf("conditional download: got %d, want 304; body: %s", w2.Code, w2.Body.String())
			}
			if lastPath != "" {
				t.Errorf("304 still hit antd (%s); the fetch must be skipped", lastPath)
			}
			if got := w2.Result().Header.Get("ETag"); got != etag {
				t.Errorf("304 ETag = %q, want %q", got, etag)
			}
		})
	}
}
