package services

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GitHub repos whose latest release we compare against.
const (
	indelibleRepo = "WithAutonomi/indelible"
	antdRepo      = "WithAutonomi/ant-sdk"
)

// versionCacheTTL bounds how often we hit the GitHub API. The check is
// on-demand (admin button), but a short cache stops repeated presses from
// burning the unauthenticated 60/hr rate limit.
const versionCacheTTL = 10 * time.Minute

// ComponentVersion is the update status of one component.
type ComponentVersion struct {
	Current         string `json:"current"`          // running version ("" if unknown)
	Latest          string `json:"latest"`           // latest release tag ("" if not checked)
	UpdateAvailable bool   `json:"update_available"` // latest is newer than current
	ReleaseURL      string `json:"release_url"`      // link to the latest release
	Checked         bool   `json:"checked"`          // false if GitHub couldn't be reached
}

// VersionCheckResult is the payload returned to the admin System page.
type VersionCheckResult struct {
	Indelible       ComponentVersion `json:"indelible"`
	Antd            ComponentVersion `json:"antd"`
	GitHubReachable bool             `json:"github_reachable"`
}

type cachedRelease struct {
	tag       string
	url       string
	fetchedAt time.Time
}

// VersionCheckService queries GitHub for the latest releases of indelible and
// antd and compares them against the running versions. It degrades gracefully:
// when GitHub is unreachable (airgapped/firewalled hosts) it reports the
// current versions with Checked=false rather than failing.
type VersionCheckService struct {
	httpClient *http.Client
	githubBase string // overridable in tests; defaults to the GitHub API

	mu    sync.Mutex
	cache map[string]cachedRelease
	now   func() time.Time // overridable in tests
}

// NewVersionCheckService creates a VersionCheckService with a short HTTP
// timeout so an unreachable GitHub can't stall the request.
func NewVersionCheckService() *VersionCheckService {
	return &VersionCheckService{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		githubBase: "https://api.github.com",
		cache:      make(map[string]cachedRelease),
		now:        time.Now,
	}
}

// Check compares the given running versions against the latest GitHub releases.
// currentAntd may be "" when the daemon version is unknown (e.g. antd
// unreachable); the antd component is then reported with Checked=false.
func (s *VersionCheckService) Check(ctx context.Context, currentIndelible, currentAntd string) VersionCheckResult {
	indLatest, indURL, indErr := s.latestRelease(ctx, indelibleRepo)
	antdLatest, antdURL, antdErr := s.latestRelease(ctx, antdRepo)

	res := VersionCheckResult{
		Indelible:       buildComponent(currentIndelible, indLatest, indURL, indErr),
		Antd:            buildComponent(currentAntd, antdLatest, antdURL, antdErr),
		GitHubReachable: indErr == nil || antdErr == nil,
	}
	return res
}

func buildComponent(current, latest, url string, err error) ComponentVersion {
	c := ComponentVersion{Current: current}
	if err != nil || latest == "" {
		return c // Checked stays false; current still surfaced
	}
	c.Latest = latest
	c.ReleaseURL = url
	c.Checked = true
	c.UpdateAvailable = isNewer(latest, current)
	return c
}

// latestRelease returns the tag_name and html_url of a repo's latest release,
// memoised for versionCacheTTL.
func (s *VersionCheckService) latestRelease(ctx context.Context, repo string) (tag, url string, err error) {
	s.mu.Lock()
	if c, ok := s.cache[repo]; ok && s.now().Sub(c.fetchedAt) < versionCacheTTL {
		s.mu.Unlock()
		return c.tag, c.url, nil
	}
	s.mu.Unlock()

	tag, url, err = s.fetchLatestRelease(ctx, repo)
	if err != nil {
		return "", "", err
	}

	s.mu.Lock()
	s.cache[repo] = cachedRelease{tag: tag, url: url, fetchedAt: s.now()}
	s.mu.Unlock()
	return tag, url, nil
}

func (s *VersionCheckService) fetchLatestRelease(ctx context.Context, repo string) (tag, url string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.githubBase+"/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return "", "", err
	}
	// GitHub requires a User-Agent and recommends pinning the API version.
	req.Header.Set("User-Agent", "indelible-version-check")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", &gitHubError{status: resp.StatusCode}
	}

	var body struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", "", err
	}
	return body.TagName, body.HTMLURL, nil
}

type gitHubError struct{ status int }

func (e *gitHubError) Error() string { return "github API returned status " + strconv.Itoa(e.status) }

// isNewer reports whether release tag `latest` is a newer semver than
// `current`. Both may carry a leading "v" and a pre-release/build suffix
// (ignored). If either can't be parsed as semver (e.g. "dev" builds), it
// returns false — better to under-report than to nag with a bogus update.
func isNewer(latest, current string) bool {
	lp, lok := parseSemver(latest)
	cp, cok := parseSemver(current)
	if !lok || !cok {
		return false
	}
	for i := range 3 {
		if lp[i] != cp[i] {
			return lp[i] > cp[i]
		}
	}
	return false
}

// parseSemver extracts major.minor.patch from a tag like "v1.2.3" or
// "1.2.3-rc1". Missing components default to 0; ok is false if there's no
// leading numeric component at all.
func parseSemver(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	// Drop pre-release (-) and build (+) metadata.
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return out, false
	}
	parts := strings.Split(v, ".")
	for i := 0; i < len(parts) && i < 3; i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			if i == 0 {
				return out, false
			}
			break
		}
		out[i] = n
	}
	return out, true
}
