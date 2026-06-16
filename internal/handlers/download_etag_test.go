package handlers

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/WithAutonomi/indelible/internal/services"
)

// White-box tests for the V2-516 download cache-validator helpers.

func TestDownloadETag(t *testing.T) {
	dataMap := &services.Upload{DataMap: sql.NullString{String: "deadbeefdatamap", Valid: true}}
	addr := &services.Upload{DatamapAddress: sql.NullString{String: "0xpublicaddr", Valid: true}}
	none := &services.Upload{}

	etag := downloadETag(dataMap)
	if etag == "" {
		t.Fatal("expected an ETag for an upload with a DataMap")
	}
	// Strong validator: quoted.
	if !strings.HasPrefix(etag, `"`) || !strings.HasSuffix(etag, `"`) {
		t.Errorf("ETag %q is not a quoted strong validator", etag)
	}
	// Must NOT leak the DataMap itself — it's the retrieval capability.
	if strings.Contains(etag, "deadbeefdatamap") {
		t.Errorf("ETag %q leaks the raw DataMap", etag)
	}
	// Stable: same content → same ETag.
	if again := downloadETag(dataMap); again != etag {
		t.Errorf("ETag not stable: %q vs %q", etag, again)
	}
	// Different content identifier → different ETag.
	if downloadETag(addr) == etag {
		t.Error("distinct content identifiers produced the same ETag")
	}
	// No identifier → no ETag (caller skips cache headers).
	if downloadETag(none) != "" {
		t.Error("expected empty ETag when no DataMap/address is present")
	}
}

func TestEtagMatches(t *testing.T) {
	const etag = `"abc123"`
	cases := []struct {
		name        string
		ifNoneMatch string
		want        bool
	}{
		{"empty", "", false},
		{"exact", `"abc123"`, true},
		{"wildcard", "*", true},
		{"in list", `"x", "abc123", "y"`, true},
		{"weak prefix", `W/"abc123"`, true},
		{"no match", `"nope"`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := etagMatches(tc.ifNoneMatch, etag); got != tc.want {
				t.Errorf("etagMatches(%q, %q) = %v, want %v", tc.ifNoneMatch, etag, got, tc.want)
			}
		})
	}
}
