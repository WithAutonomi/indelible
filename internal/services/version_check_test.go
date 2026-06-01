package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v0.9.1", "v0.9.0", true},
		{"v0.10.0", "v0.9.9", true},
		{"v1.0.0", "v0.9.9", true},
		{"v0.9.0", "v0.9.0", false},
		{"v0.9.0", "v0.9.1", false}, // current ahead
		{"0.9.1", "v0.9.0", true},   // mixed v-prefix
		{"v0.9.1-rc1", "v0.9.0", true},
		{"v0.9.0", "dev", false},  // unparseable current → don't nag
		{"garbage", "v0.9.0", false},
	}
	for _, c := range cases {
		if got := isNewer(c.latest, c.current); got != c.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestVersionCheckUpdateAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a newer tag for whichever repo is asked.
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"v9.9.9","html_url":"https://example.com/release"}`))
	}))
	defer srv.Close()

	svc := NewVersionCheckService()
	svc.githubBase = srv.URL

	res := svc.Check(context.Background(), "v0.9.0", "0.9.0")
	if !res.GitHubReachable {
		t.Fatal("expected GitHubReachable=true")
	}
	if !res.Indelible.Checked || !res.Indelible.UpdateAvailable {
		t.Errorf("indelible: expected checked+update_available, got %+v", res.Indelible)
	}
	if res.Indelible.Latest != "v9.9.9" || res.Indelible.ReleaseURL == "" {
		t.Errorf("indelible: expected latest/url populated, got %+v", res.Indelible)
	}
	if !res.Antd.UpdateAvailable {
		t.Errorf("antd: expected update_available, got %+v", res.Antd)
	}
}

func TestVersionCheckUpToDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v0.9.0","html_url":"https://example.com/r"}`))
	}))
	defer srv.Close()

	svc := NewVersionCheckService()
	svc.githubBase = srv.URL

	res := svc.Check(context.Background(), "v0.9.0", "0.9.0")
	if res.Indelible.UpdateAvailable {
		t.Errorf("expected up-to-date, got %+v", res.Indelible)
	}
	if !res.Indelible.Checked {
		t.Error("expected checked=true when GitHub responded")
	}
}

func TestVersionCheckGitHubUnreachable(t *testing.T) {
	// Point at a closed server so the fetch fails — must degrade, not error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	svc := NewVersionCheckService()
	svc.githubBase = url
	svc.httpClient.Timeout = 500 * time.Millisecond

	res := svc.Check(context.Background(), "v0.9.0", "0.9.0")
	if res.GitHubReachable {
		t.Error("expected GitHubReachable=false")
	}
	if res.Indelible.Checked || res.Indelible.UpdateAvailable {
		t.Errorf("expected unchecked/no-update, got %+v", res.Indelible)
	}
	if res.Indelible.Current != "v0.9.0" {
		t.Errorf("expected current still surfaced, got %q", res.Indelible.Current)
	}
}

func TestVersionCheckCaches(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"tag_name":"v1.0.0","html_url":"u"}`))
	}))
	defer srv.Close()

	svc := NewVersionCheckService()
	svc.githubBase = srv.URL

	svc.Check(context.Background(), "v0.9.0", "v0.9.0")
	first := calls
	svc.Check(context.Background(), "v0.9.0", "v0.9.0")
	if calls != first {
		t.Errorf("expected cache hit on second Check (no new calls), calls went %d -> %d", first, calls)
	}
}
