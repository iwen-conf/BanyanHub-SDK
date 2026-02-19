package sdk

import "time"

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
	Enabled       bool
	AutoUpdate    bool
	CheckInterval time.Duration
	Platform      string
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
	if c.OTA.Platform == "" {
		c.OTA.Platform = "universal"
	}
}
