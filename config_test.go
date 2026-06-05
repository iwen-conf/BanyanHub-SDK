package sdk

import (
	"errors"
	"runtime"
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

func TestConfig_OTAPlatformPartialDefaults(t *testing.T) {
	cfg := Config{
		OTA: OTAConfig{
			OS: "linux",
		},
	}
	cfg.setDefaults()

	if cfg.OTA.OS != "linux" {
		t.Fatalf("OTA.OS should not be overridden, got %q", cfg.OTA.OS)
	}
	if cfg.OTA.Arch != runtime.GOARCH {
		t.Fatalf("OTA.Arch = %q, want runtime arch %q", cfg.OTA.Arch, runtime.GOARCH)
	}

	cfg = Config{
		OTA: OTAConfig{
			Arch: "amd64",
		},
	}
	cfg.setDefaults()

	if cfg.OTA.OS != runtime.GOOS {
		t.Fatalf("OTA.OS = %q, want runtime OS %q", cfg.OTA.OS, runtime.GOOS)
	}
	if cfg.OTA.Arch != "amd64" {
		t.Fatalf("OTA.Arch should not be overridden, got %q", cfg.OTA.Arch)
	}
}

func TestConfig_NonPositiveOperationalValuesUseDefaults(t *testing.T) {
	cfg := Config{
		HeartbeatInterval: -1 * time.Second,
		GracePolicy: GracePolicy{
			MaxOfflineDuration: -1 * time.Second,
			WarningInterval:    -1 * time.Second,
		},
		OTA: OTAConfig{
			CheckInterval:    -1 * time.Second,
			DownloadTimeout:  -1 * time.Second,
			MaxArtifactBytes: -1,
		},
	}
	cfg.setDefaults()

	if cfg.HeartbeatInterval != time.Hour {
		t.Fatalf("HeartbeatInterval = %v, want 1h", cfg.HeartbeatInterval)
	}
	if cfg.GracePolicy.MaxOfflineDuration != 72*time.Hour {
		t.Fatalf("MaxOfflineDuration = %v, want 72h", cfg.GracePolicy.MaxOfflineDuration)
	}
	if cfg.GracePolicy.WarningInterval != 4*time.Hour {
		t.Fatalf("WarningInterval = %v, want 4h", cfg.GracePolicy.WarningInterval)
	}
	if cfg.OTA.CheckInterval != 6*time.Hour {
		t.Fatalf("CheckInterval = %v, want 6h", cfg.OTA.CheckInterval)
	}
	if cfg.OTA.DownloadTimeout != 10*time.Minute {
		t.Fatalf("DownloadTimeout = %v, want 10m", cfg.OTA.DownloadTimeout)
	}
	if cfg.OTA.MaxArtifactBytes != 500*1024*1024 {
		t.Fatalf("MaxArtifactBytes = %d, want 500MB", cfg.OTA.MaxArtifactBytes)
	}
}

func TestNormalizeServerURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "empty uses default",
			raw:  "",
			want: DefaultServerURL,
		},
		{
			name: "trims whitespace and trailing slash",
			raw:  " https://api.example.com/ ",
			want: "https://api.example.com",
		},
		{
			name: "preserves path prefix without trailing slash",
			raw:  "http://localhost:8787/base/",
			want: "http://localhost:8787/base",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeServerURL(tt.raw)
			if err != nil {
				t.Fatalf("normalizeServerURL returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizeServerURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestNormalizeServerURLRejectsInvalidValues(t *testing.T) {
	tests := []string{
		"url",
		"ftp://guard.example.com",
		"https://",
		"https://guard.example.com?tenant=one",
		"https://guard.example.com#fragment",
	}

	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			_, err := normalizeServerURL(raw)
			if !errors.Is(err, ErrInvalidServerURL) {
				t.Fatalf("expected ErrInvalidServerURL, got %v", err)
			}
		})
	}
}

func TestServerURLForPath(t *testing.T) {
	tests := []struct {
		name      string
		serverURL string
		path      string
		want      string
	}{
		{
			name:      "absolute url passes through",
			serverURL: "https://guard.example.com",
			path:      "https://cdn.example.com/artifact.bin",
			want:      "https://cdn.example.com/artifact.bin",
		},
		{
			name:      "relative path gets leading slash",
			serverURL: "https://guard.example.com",
			path:      "api/v1/update/download",
			want:      "https://guard.example.com/api/v1/update/download",
		},
		{
			name:      "preserves server base path",
			serverURL: "https://guard.example.com/base",
			path:      "/api/v1/update/download",
			want:      "https://guard.example.com/base/api/v1/update/download",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := serverURLForPath(tt.serverURL, tt.path); got != tt.want {
				t.Fatalf("serverURLForPath(%q, %q) = %q, want %q", tt.serverURL, tt.path, got, tt.want)
			}
		})
	}
}
