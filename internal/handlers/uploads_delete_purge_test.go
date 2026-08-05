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

	"github.com/WithAutonomi/indelible/internal/downloadcache"
)

// V2-824 delete-purge invariant tests: deleting an upload must remove its
// cached plaintext from this instance's disk before the delete API returns,
// and a purge that cannot unlink must fail the delete with the row intact.

func doDelete(router http.Handler, token, uuid string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("DELETE", "/api/v2/uploads/"+uuid, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// warmCache downloads uuid once so the read-through path promotes it, and
// returns the promoted file's on-disk path (derived from the response ETag).
func warmCache(t *testing.T, router http.Handler, token, uuid, dataDir, content string) string {
	t.Helper()
	w := doDownload(router, token, uuid, "")
	if w.Code != http.StatusOK || w.Body.String() != content {
		t.Fatalf("warming download: %d %q", w.Code, w.Body.String())
	}
	key := strings.Trim(w.Result().Header.Get("ETag"), `"`)
	if key == "" {
		t.Fatal("missing ETag on warming download")
	}
	p := filepath.Join(dataDir, "cache", "objects", key[0:2], key[2:4], key)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("cache not warmed at %s: %v", p, err)
	}
	return p
}

func TestDeleteUpload_PurgesCachedCopy(t *testing.T) {
	const content = "cached then deleted"
	fake := newCacheFakeAntd(t, content)
	router, token, cfg, db, store := newCacheTestEnvWithStore(t, fake.srv.URL, map[string]string{
		"download_cache_max_bytes": "1048576",
	})
	uuid := makeUpload(t, router, db, token, "purge-me.txt", "public", "addr-purge")
	cached := warmCache(t, router, token, uuid, cfg.DataDir, content)

	if w := doDelete(router, token, uuid); w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}

	// The invariant: by the time the delete returned, the plaintext was gone.
	if _, err := os.Stat(cached); !os.IsNotExist(err) {
		t.Fatalf("cached plaintext outlived the delete: %v", err)
	}
	if count, _ := store.Stats(); count != 0 {
		t.Fatalf("cache still indexes %d entries after delete", count)
	}
	if got := store.Metrics().Purged.Load(); got != 1 {
		t.Fatalf("Purged = %d, want 1", got)
	}
	if w := doDownload(router, token, uuid, ""); w.Code != http.StatusNotFound {
		t.Fatalf("deleted upload still downloadable: %d", w.Code)
	}
}

func TestDeleteUpload_FailsWhenPurgeFails(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	const content = "must not be reported deleted"
	fake := newCacheFakeAntd(t, content)
	router, token, cfg, db, store := newCacheTestEnvWithStore(t, fake.srv.URL, map[string]string{
		"download_cache_max_bytes": "1048576",
	})
	uuid := makeUpload(t, router, db, token, "stuck.txt", "public", "addr-stuck")
	cached := warmCache(t, router, token, uuid, cfg.DataDir, content)

	parent := filepath.Dir(cached)
	if err := os.Chmod(parent, 0500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0700) })

	// Purge cannot unlink → the delete must fail with the row intact: the API
	// must never report a deletion while the plaintext is still readable here.
	if w := doDelete(router, token, uuid); w.Code != http.StatusInternalServerError {
		t.Fatalf("delete with unpurgeable cache: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(cached); err != nil {
		t.Fatalf("cache file should still exist: %v", err)
	}
	if count, _ := store.Stats(); count != 1 {
		t.Fatalf("entry must stay indexed after failed purge: count=%d", count)
	}
	if w := doDownload(router, token, uuid, ""); w.Code != http.StatusOK {
		t.Fatalf("row must survive a failed purge: %d", w.Code)
	}

	// Clear the obstruction: the retry deletes cleanly.
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatalf("chmod restore: %v", err)
	}
	if w := doDelete(router, token, uuid); w.Code != http.StatusOK {
		t.Fatalf("retry delete: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(cached); !os.IsNotExist(err) {
		t.Fatalf("cached plaintext outlived the retry delete: %v", err)
	}
}

// The resurrection race: a download that passed its DB check before the
// delete committed promotes its fetched bytes AFTER the delete's purge ran.
// The promote-site guard must re-verify the row and take the bytes back out.
func TestDeleteUpload_ResurrectionGuard(t *testing.T) {
	const content = "promoted after my row died"
	var midFetch atomic.Value // func(), set once the env exists

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/files/public/get" {
			http.Error(w, "{}", http.StatusNotFound)
			return
		}
		var body struct {
			DestPath string `json:"dest_path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.DestPath == "" {
			http.Error(w, "{}", http.StatusBadRequest)
			return
		}
		// The delete lands while the fetch is in flight — after the download
		// handler resolved the row, before the promotion.
		if f, ok := midFetch.Load().(func()); ok && f != nil {
			f()
		}
		if err := os.WriteFile(body.DestPath, []byte(content), 0600); err != nil {
			http.Error(w, "{}", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	router, token, cfg, db, store := newCacheTestEnvWithStore(t, srv.URL, map[string]string{
		"download_cache_max_bytes": "1048576",
	})
	uuid := makeUpload(t, router, db, token, "ghost.txt", "public", "addr-ghost")
	midFetch.Store(func() {
		// The committed row delete, as the delete handler leaves it: its own
		// purges found nothing (the entry didn't exist yet), the row is gone.
		if _, err := db.Exec("DELETE FROM uploads WHERE uuid = ?", uuid); err != nil {
			t.Errorf("mid-fetch delete: %v", err)
		}
	})

	// The in-flight download still completes — it predates the delete.
	if w := doDownload(router, token, uuid, ""); w.Code != http.StatusOK || w.Body.String() != content {
		t.Fatalf("in-flight download: code=%d", doDownload(router, token, uuid, "").Code)
	}

	// But its promotion must not have outlived the row. (The empty fanout
	// directory skeleton is expected to remain — Drop unlinks files only.)
	if count, bytes := store.Stats(); count != 0 || bytes != 0 {
		t.Fatalf("resurrected cache entry: (%d, %d)", count, bytes)
	}
	key := downloadcache.KeyForIdentifier("addr-ghost")
	cached := filepath.Join(cfg.DataDir, "cache", "objects", key[0:2], key[2:4], key)
	if _, err := os.Stat(cached); !os.IsNotExist(err) {
		t.Fatalf("resurrected cache bytes on disk at %s: %v", cached, err)
	}
	if got := store.Metrics().Purged.Load(); got != 1 {
		t.Fatalf("Purged = %d, want 1 (the guard's drop)", got)
	}
}
