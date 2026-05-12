package migrate

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/WithAutonomi/indelible/internal/database"
	"github.com/WithAutonomi/indelible/internal/services"
)

// fakePublisher records ChunkPut calls and returns scripted addresses / errors.
type fakePublisher struct {
	addr   func(payload []byte) string // address for a given payload (default: hex of first 16 bytes left-padded)
	putErr error                       // returned by every ChunkPut once non-nil
	stored map[string][]byte           // address → bytes (for ChunkGet round-trips)
	calls  int
}

func newFake() *fakePublisher {
	return &fakePublisher{
		stored: map[string][]byte{},
		addr: func(p []byte) string {
			// deterministic stand-in for content addressing: hex-encode the
			// payload itself, truncated/padded to 64 chars.
			h := hex.EncodeToString(p)
			if len(h) >= 64 {
				return h[:64]
			}
			return h + strings.Repeat("0", 64-len(h))
		},
	}
}

func (f *fakePublisher) ChunkPut(_ context.Context, data []byte) (string, error) {
	f.calls++
	if f.putErr != nil {
		return "", f.putErr
	}
	addr := f.addr(data)
	cp := make([]byte, len(data))
	copy(cp, data)
	f.stored[addr] = cp
	return addr, nil
}

func (f *fakePublisher) ChunkGet(_ context.Context, address string) ([]byte, error) {
	data, ok := f.stored[address]
	if !ok {
		return nil, errors.New("not found")
	}
	return data, nil
}

// seedCompletedPrivateUpload creates a queued upload then transitions it to
// completed with a hex-encoded data_map blob. Returns the row's UUID for the
// test to refer back to.
func seedCompletedPrivateUpload(t *testing.T, svc *services.UploadService, userID int64, name string, payload []byte) string {
	t.Helper()
	u, err := svc.Create(userID, nil, name, name, int64(len(payload)), "application/octet-stream", "private", "/tmp/"+name, nil)
	if err != nil {
		t.Fatalf("seed create %s: %v", name, err)
	}
	if err := svc.MarkCompleted(u.ID, hex.EncodeToString(payload), "100"); err != nil {
		t.Fatalf("seed mark-completed %s: %v", name, err)
	}
	return u.UUID
}

// setupDB mirrors services.setupTestDB without taking a dependency on that
// package's *_test.go helpers (which are not exported).
func setupDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Open("sqlite://:memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db, "sqlite"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newUser(t *testing.T, db *sql.DB, email string) int64 {
	t.Helper()
	u, err := services.NewUserService(db).Create(email, "hashed_pw", "T", "U")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	return u.ID
}

func TestPublishDataMaps_HappyPath(t *testing.T) {
	db := setupDB(t)
	uid := newUser(t, db, "happy@example.com")
	svc := services.NewUploadService(db)

	wantUUIDs := []string{
		seedCompletedPrivateUpload(t, svc, uid, "a.pdf", []byte("alpha-datamap-bytes")),
		seedCompletedPrivateUpload(t, svc, uid, "b.pdf", []byte("bravo-datamap-bytes")),
		seedCompletedPrivateUpload(t, svc, uid, "c.pdf", []byte("charlie-datamap-bytes")),
	}

	fake := newFake()
	var buf bytes.Buffer
	run, err := PublishDataMaps(context.Background(), svc, fake, PublishDataMapsOptions{}, &buf)
	if err != nil {
		t.Fatalf("PublishDataMaps: %v", err)
	}

	if run.Candidates != 3 || run.Published != 3 || run.Failed != 0 {
		t.Errorf("summary: candidates=%d published=%d failed=%d, want 3/3/0", run.Candidates, run.Published, run.Failed)
	}
	if fake.calls != 3 {
		t.Errorf("ChunkPut called %d times, want 3", fake.calls)
	}

	// Each row should be public with a populated datamap_address.
	for _, uuid := range wantUUIDs {
		got, err := svc.GetByUUID(uuid)
		if err != nil {
			t.Fatalf("get %s: %v", uuid, err)
		}
		if got.Visibility != "public" {
			t.Errorf("%s: visibility=%q, want public", uuid, got.Visibility)
		}
		if !got.DatamapAddress.Valid || got.DatamapAddress.String == "" {
			t.Errorf("%s: datamap_address not set", uuid)
		}
	}

	// JSONLines progress: one record per row, all with outcome="published".
	got := strings.Count(buf.String(), `"outcome":"published"`)
	if got != 3 {
		t.Errorf("progress: %d published outcomes, want 3 (output: %q)", got, buf.String())
	}
}

func TestPublishDataMaps_DryRunMakesNoChanges(t *testing.T) {
	db := setupDB(t)
	uid := newUser(t, db, "dry@example.com")
	svc := services.NewUploadService(db)
	uuid := seedCompletedPrivateUpload(t, svc, uid, "a.pdf", []byte("payload"))

	fake := newFake()
	run, err := PublishDataMaps(context.Background(), svc, fake, PublishDataMapsOptions{DryRun: true}, nil)
	if err != nil {
		t.Fatalf("PublishDataMaps: %v", err)
	}
	if run.Candidates != 1 || run.Published != 0 {
		t.Errorf("dry-run summary: cand=%d pub=%d, want 1/0", run.Candidates, run.Published)
	}
	if fake.calls != 0 {
		t.Errorf("dry-run still called ChunkPut %d times", fake.calls)
	}
	got, _ := svc.GetByUUID(uuid)
	if got.Visibility != "private" || got.DatamapAddress.Valid {
		t.Errorf("dry-run mutated row: visibility=%s datamap_address.valid=%v",
			got.Visibility, got.DatamapAddress.Valid)
	}
}

func TestPublishDataMaps_WalletMissingAbortsRun(t *testing.T) {
	db := setupDB(t)
	uid := newUser(t, db, "wallet@example.com")
	svc := services.NewUploadService(db)
	first := seedCompletedPrivateUpload(t, svc, uid, "a.pdf", []byte("first"))
	second := seedCompletedPrivateUpload(t, svc, uid, "b.pdf", []byte("second"))
	_ = first
	_ = second

	fake := newFake()
	fake.putErr = errors.New("antd: 503 service unavailable — wallet not configured — set AUTONOMI_WALLET_KEY")

	run, err := PublishDataMaps(context.Background(), svc, fake, PublishDataMapsOptions{}, nil)
	if !errors.Is(err, ErrDaemonWalletMissing) {
		t.Fatalf("expected ErrDaemonWalletMissing, got %v", err)
	}
	if fake.calls != 1 {
		t.Errorf("expected to bail after 1 call, got %d", fake.calls)
	}
	if run.Failed != 1 {
		t.Errorf("Failed counter: %d, want 1", run.Failed)
	}

	// Neither row should have been flipped.
	for _, uuid := range []string{first, second} {
		got, _ := svc.GetByUUID(uuid)
		if got.Visibility != "private" || got.DatamapAddress.Valid {
			t.Errorf("%s mutated after wallet-missing abort", uuid)
		}
	}
}

func TestPublishDataMaps_GenericErrorContinues(t *testing.T) {
	db := setupDB(t)
	uid := newUser(t, db, "gen@example.com")
	svc := services.NewUploadService(db)
	seedCompletedPrivateUpload(t, svc, uid, "a.pdf", []byte("alpha"))
	seedCompletedPrivateUpload(t, svc, uid, "b.pdf", []byte("bravo"))

	// scriptedPublisher fails the first ChunkPut, then delegates to the fake.
	scripted := &scriptedPublisher{inner: newFake(), failFirst: errors.New("transient network blip")}

	run, err := PublishDataMaps(context.Background(), svc, scripted, PublishDataMapsOptions{}, nil)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if run.Failed != 1 || run.Published != 1 {
		t.Errorf("summary failed=%d published=%d, want 1/1", run.Failed, run.Published)
	}
}

// scriptedPublisher fails the first ChunkPut with failFirst, then delegates.
type scriptedPublisher struct {
	inner     *fakePublisher
	failFirst error
	calls     int
}

func (s *scriptedPublisher) ChunkPut(ctx context.Context, data []byte) (string, error) {
	s.calls++
	if s.calls == 1 && s.failFirst != nil {
		return "", s.failFirst
	}
	return s.inner.ChunkPut(ctx, data)
}

func (s *scriptedPublisher) ChunkGet(ctx context.Context, address string) ([]byte, error) {
	return s.inner.ChunkGet(ctx, address)
}

func TestPublishDataMaps_VerifyDetectsMismatch(t *testing.T) {
	db := setupDB(t)
	uid := newUser(t, db, "verify@example.com")
	svc := services.NewUploadService(db)
	uuid := seedCompletedPrivateUpload(t, svc, uid, "a.pdf", []byte("the real data map"))

	// Publisher whose ChunkGet returns different bytes than ChunkPut accepted.
	bad := &corruptingPublisher{inner: newFake()}

	run, err := PublishDataMaps(context.Background(), svc, bad, PublishDataMapsOptions{Verify: true}, nil)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if run.Failed != 1 || run.Verified != 0 {
		t.Errorf("summary failed=%d verified=%d, want 1/0", run.Failed, run.Verified)
	}
	got, _ := svc.GetByUUID(uuid)
	if got.Visibility != "private" {
		t.Errorf("verify failure still flipped visibility to %s", got.Visibility)
	}
}

type corruptingPublisher struct{ inner *fakePublisher }

func (c *corruptingPublisher) ChunkPut(ctx context.Context, data []byte) (string, error) {
	return c.inner.ChunkPut(ctx, data)
}
func (c *corruptingPublisher) ChunkGet(ctx context.Context, address string) ([]byte, error) {
	b, err := c.inner.ChunkGet(ctx, address)
	if err != nil {
		return nil, err
	}
	out := append([]byte{}, b...)
	out[0] ^= 0xff
	return out, nil
}

func TestPublishDataMaps_LimitAndUUIDFilter(t *testing.T) {
	db := setupDB(t)
	uid := newUser(t, db, "filt@example.com")
	svc := services.NewUploadService(db)
	for i := 0; i < 5; i++ {
		seedCompletedPrivateUpload(t, svc, uid, fmt.Sprintf("f%d.pdf", i), []byte(fmt.Sprintf("payload-%d", i)))
	}

	fake := newFake()
	run, err := PublishDataMaps(context.Background(), svc, fake, PublishDataMapsOptions{Limit: 2}, nil)
	if err != nil || run.Published != 2 {
		t.Fatalf("limit=2: err=%v published=%d", err, run.Published)
	}

	// Now pick a specific UUID from the remaining un-migrated rows.
	remaining, _ := svc.ListPrivatePublishCandidates(0)
	if len(remaining) != 3 {
		t.Fatalf("expected 3 remaining, got %d", len(remaining))
	}
	target := remaining[1].UUID

	fake = newFake()
	run, err = PublishDataMaps(context.Background(), svc, fake, PublishDataMapsOptions{UUID: target}, nil)
	if err != nil || run.Published != 1 || run.Candidates != 1 {
		t.Fatalf("uuid filter: err=%v pub=%d cand=%d", err, run.Published, run.Candidates)
	}
	got, _ := svc.GetByUUID(target)
	if got.Visibility != "public" {
		t.Errorf("uuid-filter target still private")
	}
}

func TestPublishDataMaps_SkipsBadHex(t *testing.T) {
	db := setupDB(t)
	uid := newUser(t, db, "bad@example.com")
	svc := services.NewUploadService(db)
	// Seed a completed private upload, then corrupt its data_map to non-hex.
	uuid := seedCompletedPrivateUpload(t, svc, uid, "x.pdf", []byte("ok"))
	got, _ := svc.GetByUUID(uuid)
	_, err := db.Exec(`UPDATE uploads SET data_map = 'zzz-not-hex' WHERE id = ?`, got.ID)
	if err != nil {
		t.Fatalf("corrupt: %v", err)
	}

	fake := newFake()
	run, err := PublishDataMaps(context.Background(), svc, fake, PublishDataMapsOptions{}, nil)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if run.Failed != 1 || run.Published != 0 || fake.calls != 0 {
		t.Errorf("bad-hex: failed=%d published=%d publisher_calls=%d, want 1/0/0",
			run.Failed, run.Published, fake.calls)
	}
}

func TestIsWalletMissing(t *testing.T) {
	cases := []struct {
		err  string
		want bool
	}{
		{"wallet not configured — set AUTONOMI_WALLET_KEY", true},
		{"503 Service Unavailable: wallet not configured", true},
		{"missing autonomi_wallet_key env", true},
		{"connection refused", false},
		{"bad request: chunk too large", false},
	}
	for _, c := range cases {
		got := isWalletMissing(errors.New(c.err))
		if got != c.want {
			t.Errorf("isWalletMissing(%q) = %v, want %v", c.err, got, c.want)
		}
	}
}
