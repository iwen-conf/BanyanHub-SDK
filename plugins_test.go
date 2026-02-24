package sdk

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func buildTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		data := []byte(content)
		h := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(data)),
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatalf("write tar body: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return buf.Bytes()
}

func TestGetPluginCatalog_Success(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/plugins/catalog" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("license_key") == "" {
			t.Fatalf("expected license_key query")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"project_slug":  "myproj",
			"machine_id":    "machine-1",
			"source_os":     "linux",
			"source_arch":   "amd64",
			"update_frozen": false,
			"plugins": []map[string]any{
				{
					"slug":              "admin-frontend",
					"name":              "Admin Frontend",
					"type":              "frontend",
					"ota_enabled":       true,
					"installed_version": "1.0.0",
					"latest_version":    "1.1.0",
					"update_available":  true,
					"can_update":        true,
				},
			},
		})
	}))
	defer srv.Close()

	g, err := New(Config{
		ServerURL:     srv.URL,
		LicenseKey:    "LIC-1",
		PublicKeyPEM:  pemEncodePublicKey(pubKey),
		ProjectSlug:   "myproj",
		ComponentSlug: "backend",
	})
	if err != nil {
		t.Fatalf("new guard: %v", err)
	}

	catalog, err := g.GetPluginCatalog(context.Background(), true)
	if err != nil {
		t.Fatalf("get plugin catalog: %v", err)
	}

	if catalog.ProjectSlug != "myproj" {
		t.Fatalf("unexpected project slug: %s", catalog.ProjectSlug)
	}
	if len(catalog.Plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(catalog.Plugins))
	}
	if catalog.Plugins[0].Slug != "admin-frontend" {
		t.Fatalf("unexpected plugin slug: %s", catalog.Plugins[0].Slug)
	}
	if !catalog.Plugins[0].UpdateAvailable {
		t.Fatal("expected update_available=true")
	}
}

func TestCheckPluginUpdates_FiltersAvailableOnly(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"project_slug":  "myproj",
			"machine_id":    "machine-1",
			"source_os":     "linux",
			"source_arch":   "amd64",
			"update_frozen": false,
			"plugins": []map[string]any{
				{
					"slug":             "a",
					"name":             "A",
					"type":             "frontend",
					"ota_enabled":      true,
					"update_available": true,
					"can_update":       true,
				},
				{
					"slug":             "b",
					"name":             "B",
					"type":             "frontend",
					"ota_enabled":      true,
					"update_available": false,
					"can_update":       false,
				},
			},
		})
	}))
	defer srv.Close()

	g, err := New(Config{
		ServerURL:     srv.URL,
		LicenseKey:    "LIC-1",
		PublicKeyPEM:  pemEncodePublicKey(pubKey),
		ProjectSlug:   "myproj",
		ComponentSlug: "backend",
	})
	if err != nil {
		t.Fatalf("new guard: %v", err)
	}

	updates, err := g.CheckPluginUpdates(context.Background())
	if err != nil {
		t.Fatalf("check plugin updates: %v", err)
	}
	if len(updates) != 1 {
		t.Fatalf("expected 1 updatable plugin, got %d", len(updates))
	}
	if updates[0].Slug != "a" {
		t.Fatalf("unexpected plugin in update list: %s", updates[0].Slug)
	}
}

func TestUpdatePlugin_FrontendSuccess(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)

	liveDir := t.TempDir()
	targetDir := filepath.Join(liveDir, "frontend-live")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("seed old file: %v", err)
	}

	tarGzBytes := buildTarGz(t, map[string]string{
		"index.html": "new-frontend",
	})
	hash := sha256.Sum256(tarGzBytes)
	hashHex := hex.EncodeToString(hash[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/plugins/catalog":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"project_slug":  "myproj",
				"machine_id":    "machine-1",
				"source_os":     "linux",
				"source_arch":   "amd64",
				"update_frozen": false,
				"plugins": []map[string]any{
					{
						"slug":              "admin-frontend",
						"name":              "Admin Frontend",
						"type":              "frontend",
						"ota_enabled":       true,
						"installed_version": "1.0.0",
						"latest_version":    "2.0.0",
						"update_available":  true,
						"can_update":        true,
					},
				},
			})
		case "/api/v1/update/download":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"download_url": "/api/v1/update/fetch/token-1",
				"sha256":       hashHex,
			})
		case "/api/v1/update/fetch/token-1":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(tarGzBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	g, err := New(Config{
		ServerURL:     srv.URL,
		LicenseKey:    "LIC-1",
		PublicKeyPEM:  pemEncodePublicKey(pubKey),
		ProjectSlug:   "myproj",
		ComponentSlug: "backend",
		OTA: OTAConfig{
			Enabled:          true,
			AutoUpdate:       false,
			OS:               "linux",
			Arch:             "amd64",
			MaxArtifactBytes: int64(len(tarGzBytes)) + 1024,
		},
		ManagedComponents: []ManagedComponent{
			{
				Slug:     "admin-frontend",
				Dir:      targetDir,
				Strategy: UpdateFrontend,
			},
		},
	})
	if err != nil {
		t.Fatalf("new guard: %v", err)
	}
	g.SetManagedVersion("admin-frontend", "1.0.0")

	if err := g.UpdatePlugin(context.Background(), "admin-frontend"); err != nil {
		t.Fatalf("manual update failed: %v", err)
	}

	if got := g.currentManagedVersion("admin-frontend"); got != "2.0.0" {
		t.Fatalf("expected managed version 2.0.0, got %s", got)
	}

	newContent, err := os.ReadFile(filepath.Join(targetDir, "index.html"))
	if err != nil {
		t.Fatalf("read extracted frontend file: %v", err)
	}
	if string(newContent) != "new-frontend" {
		t.Fatalf("unexpected extracted content: %s", string(newContent))
	}
}

func TestUpdatePlugin_ErrorCases(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)

	t.Run("frozen", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"project_slug":  "myproj",
				"machine_id":    "machine-1",
				"source_os":     "linux",
				"source_arch":   "amd64",
				"update_frozen": true,
				"plugins":       []map[string]any{},
			})
		}))
		defer srv.Close()

		g, err := New(Config{
			ServerURL:     srv.URL,
			LicenseKey:    "LIC-1",
			PublicKeyPEM:  pemEncodePublicKey(pubKey),
			ProjectSlug:   "myproj",
			ComponentSlug: "backend",
		})
		if err != nil {
			t.Fatalf("new guard: %v", err)
		}

		err = g.UpdatePlugin(context.Background(), "any")
		if err != ErrUpdateFrozen {
			t.Fatalf("expected ErrUpdateFrozen, got %v", err)
		}
	})

	t.Run("not_managed", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"project_slug":  "myproj",
				"machine_id":    "machine-1",
				"source_os":     "linux",
				"source_arch":   "amd64",
				"update_frozen": false,
				"plugins": []map[string]any{
					{
						"slug":             "unmanaged-plugin",
						"name":             "Unmanaged",
						"type":             "frontend",
						"ota_enabled":      true,
						"latest_version":   "1.0.1",
						"update_available": true,
						"can_update":       true,
					},
				},
			})
		}))
		defer srv.Close()

		g, err := New(Config{
			ServerURL:     srv.URL,
			LicenseKey:    "LIC-1",
			PublicKeyPEM:  pemEncodePublicKey(pubKey),
			ProjectSlug:   "myproj",
			ComponentSlug: "backend",
		})
		if err != nil {
			t.Fatalf("new guard: %v", err)
		}

		err = g.UpdatePlugin(context.Background(), "unmanaged-plugin")
		if err != ErrPluginNotManaged {
			t.Fatalf("expected ErrPluginNotManaged, got %v", err)
		}
	})
}
