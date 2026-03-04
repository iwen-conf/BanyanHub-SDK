package sdk

import (
	"testing"
	"time"
)

func TestConfig_SetDefaults(t *testing.T) {
	cfg := Config{}
	cfg.setDefaults()

	if cfg.HeartbeatInterval != 1*time.Hour {
		t.Errorf("HeartbeatInterval: expected 1h, got %v", cfg.HeartbeatInterval)
	}

	if cfg.GracePolicy.MaxOfflineDuration != 72*time.Hour {
		t.Errorf("MaxOfflineDuration: expected 72h, got %v", cfg.GracePolicy.MaxOfflineDuration)
	}

	if cfg.GracePolicy.WarningInterval != 4*time.Hour {
		t.Errorf("WarningInterval: expected 4h, got %v", cfg.GracePolicy.WarningInterval)
	}

	if cfg.OTA.DownloadTimeout != 10*time.Minute {
		t.Errorf("DownloadTimeout: expected 10m, got %v", cfg.OTA.DownloadTimeout)
	}

	if cfg.OTA.MaxArtifactBytes != 500*1024*1024 {
		t.Errorf("MaxArtifactBytes: expected 500MB, got %v", cfg.OTA.MaxArtifactBytes)
	}
}

func TestConfig_PartialDefaults(t *testing.T) {
	cfg := Config{
		HeartbeatInterval: 30 * time.Minute,
	}
	cfg.setDefaults()

	if cfg.HeartbeatInterval != 30*time.Minute {
		t.Errorf("HeartbeatInterval should not be overridden, got %v", cfg.HeartbeatInterval)
	}

	if cfg.GracePolicy.MaxOfflineDuration != 72*time.Hour {
		t.Errorf("MaxOfflineDuration: expected 72h (default), got %v", cfg.GracePolicy.MaxOfflineDuration)
	}
}
