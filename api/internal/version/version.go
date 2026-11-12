// Package version holds build information injected at link time.
package version

// These values are overridden with -ldflags "-X" at build time.
var (
	Version = "dev"
	Commit  = "unknown"
)
