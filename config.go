package sdk

import (
	"runtime"
	"time"
)

type Config struct {
	ServerURL    string
	LicenseKey   string
	PublicKeyPEM []byte

	ProjectSlug   string
	ComponentSlug string

	HeartbeatInterval time.Duration
	GracePolicy       GracePolicy
	OTA               OTAConfig
	ManagedComponents []ManagedComponent
}

type GracePolicy struct {
	MaxOfflineDuration time.Duration
	WarningInterval    time.Duration
}

type OTAConfig struct {
	Enabled            bool
	AutoUpdate         bool
	CheckInterval      time.Duration
	OS                 string
	Arch               string
	DownloadTimeout    time.Duration
	MaxArtifactBytes   int64
	OnUpdateProgress   func(component, stage string, progress float64)
	OnUpdateResult     func(component, oldVer, newVer string, success bool, err error)
	OnUpdateFailure    func(component string, err error)
}

type UpdateStrategy int

const (
	UpdateBackend  UpdateStrategy = iota
	UpdateFrontend
)

type ManagedComponent struct {
	Slug       string
	Dir        string
	Strategy   UpdateStrategy
	PostUpdate func() error
}

func (c *Config) setDefaults() {
	if c.HeartbeatInterval == 0 {
		c.HeartbeatInterval = 1 * time.Hour
	}
	if c.GracePolicy.MaxOfflineDuration == 0 {
		c.GracePolicy.MaxOfflineDuration = 72 * time.Hour
	}
	if c.GracePolicy.WarningInterval == 0 {
		c.GracePolicy.WarningInterval = 4 * time.Hour
	}
	if c.OTA.CheckInterval == 0 {
		c.OTA.CheckInterval = 6 * time.Hour
	}
	// Auto-detect OS and Arch from runtime if not configured
	if c.OTA.OS == "" && c.OTA.Arch == "" {
		c.OTA.OS = runtime.GOOS
		c.OTA.Arch = runtime.GOARCH
	}
	if c.OTA.DownloadTimeout == 0 {
		c.OTA.DownloadTimeout = 10 * time.Minute
	}
	if c.OTA.MaxArtifactBytes == 0 {
		c.OTA.MaxArtifactBytes = 500 * 1024 * 1024 // 500MB
	}
}
