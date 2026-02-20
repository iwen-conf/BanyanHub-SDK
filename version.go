package sdk

// Version information, injected at build time via ldflags
var (
	// Version is the semantic version (e.g., "1.2.3")
	Version = "dev"

	// GitCommit is the git commit hash
	GitCommit = "unknown"

	// BuildTime is the build timestamp
	BuildTime = "unknown"

	// GoVersion is the Go compiler version
	GoVersion = "unknown"
)

// VersionInfo returns formatted version information
func VersionInfo() string {
	return Version + " (" + GitCommit + ", built at " + BuildTime + ")"
}
