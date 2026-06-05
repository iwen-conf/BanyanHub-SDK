package sdk

import (
	"fmt"
	"net/url"
	"runtime"
	"strings"
	"time"
)

// DefaultServerURL is the official BanyanHub cloud API endpoint.
// Consumers can override this by setting Config.ServerURL.
const DefaultServerURL = "https://guard.iluwen.cn"

type Config struct {
	ServerURL           string
	LicenseKey          string
	PublicKeyPEM        []byte
	LegacyPublicKeysPEM [][]byte

	ProjectSlug   string
	ComponentSlug string

	HeartbeatInterval time.Duration
	GracePolicy       GracePolicy
	OTA               OTAConfig
	ManagedComponents []ManagedComponent
	AllowSystemTrust  bool
	PinnedSPKIHashes  []string
}

type GracePolicy struct {
	MaxOfflineDuration time.Duration
	WarningInterval    time.Duration
}

type OTAConfig struct {
	Enabled          bool
	AutoUpdate       bool
	CheckInterval    time.Duration
	OS               string
	Arch             string
	DownloadTimeout  time.Duration
	MaxArtifactBytes int64
	OnUpdateProgress func(component, stage string, progress float64)
	OnUpdateResult   func(component, oldVer, newVer string, success bool, err error)
	OnUpdateFailure  func(component string, err error)
}

type UpdateStrategy int

const (
	UpdateBackend UpdateStrategy = iota
	UpdateFrontend
)

type ManagedComponent struct {
	Slug       string
	Dir        string
	Strategy   UpdateStrategy
	PostUpdate func() error
}

func (c *Config) setDefaults() {
	// ServerURL: use DefaultServerURL if not configured
	if c.ServerURL == "" {
		c.ServerURL = DefaultServerURL
	}
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = 1 * time.Hour
	}
	if c.GracePolicy.MaxOfflineDuration <= 0 {
		c.GracePolicy.MaxOfflineDuration = 72 * time.Hour
	}
	if c.GracePolicy.WarningInterval <= 0 {
		c.GracePolicy.WarningInterval = 4 * time.Hour
	}
	if c.OTA.CheckInterval <= 0 {
		c.OTA.CheckInterval = 6 * time.Hour
	}
	// Auto-detect OS and Arch independently so partial overrides remain valid.
	if c.OTA.OS == "" {
		c.OTA.OS = runtime.GOOS
	}
	if c.OTA.Arch == "" {
		c.OTA.Arch = runtime.GOARCH
	}
	if c.OTA.DownloadTimeout <= 0 {
		c.OTA.DownloadTimeout = 10 * time.Minute
	}
	if c.OTA.MaxArtifactBytes <= 0 {
		c.OTA.MaxArtifactBytes = 500 * 1024 * 1024 // 500MB
	}
}

func normalizeServerURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = DefaultServerURL
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("%w: must be an absolute http(s) URL", ErrInvalidServerURL)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("%w: unsupported scheme %q", ErrInvalidServerURL, parsed.Scheme)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%w: query and fragment are not allowed", ErrInvalidServerURL)
	}

	return strings.TrimRight(parsed.String(), "/"), nil
}

func serverURLForPath(serverURL, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return serverURL
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return serverURL + path
}
