// Package buildinfo exposes the indelible build version so handlers can
// surface it without importing from cmd/indelible.
//
// Version is set at link time via -ldflags
// "-X github.com/WithAutonomi/indelible/internal/buildinfo.Version=<tag>".
package buildinfo

// Version reports the indelible version. Defaults to "dev" for unstamped builds.
var Version = "dev"
