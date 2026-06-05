package sdk

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestUpdateFrontend_SuccessFullCoverage(t *testing.T) {
	pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	content := []byte("hello frontend")
	hdr := &tar.Header{Name: "frontend.txt", Mode: 0o644, Size: int64(len(content))}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("write content: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}

	tarGzBytes := buf.Bytes()
	hash := sha256.Sum256(tarGzBytes)
	hashStr := hex.EncodeToString(hash[:])
	signature := signUpdateHash(t, privKey, hashStr)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/update/download":
			json.NewEncoder(w).Encode(map[string]string{
				"download_url": "/download/frontend.tar.gz",
				"sha256":       hashStr,
				"signature":    signature,
			})
		case "/download/frontend.tar.gz":
			w.Write(tarGzBytes)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	successCalled := false
	tempDir := t.TempDir()
	targetDir := filepath.Join(tempDir, "live")

	g := &Guard{
		cfg: Config{
			ServerURL:     server.URL,
			LicenseKey:    "test-key",
			ProjectSlug:   "test-project",
			ComponentSlug: "backend",
			OTA: OTAConfig{
				AutoUpdate:       true,
				MaxArtifactBytes: 10 * 1024 * 1024,
				OnUpdateResult: func(component, oldVer, newVer string, success bool, err error) {
					successCalled = success
				},
			},
		},
		publicKey:   pubKey,
		fingerprint: &Fingerprint{machineID: "test-machine"},
		httpClient:  &http.Client{Timeout: 5 * time.Second},
		updateMu:    sync.Mutex{},
		mu:          sync.RWMutex{},
		managedVersions: map[string]string{
			"frontend": "1.0.0",
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	u := updateInfo{Component: "frontend", Latest: "2.0.0", UpdateAvailable: true}
	mc := ManagedComponent{Slug: "frontend", Dir: targetDir}

	if err := g.updateFrontend(mc, u); err != nil {
		t.Fatalf("updateFrontend failed: %v", err)
	}

	if !successCalled {
		t.Fatal("expected OnUpdateResult success callback")
	}

	extractedFile := filepath.Join(targetDir, "frontend.txt")
	data, err := os.ReadFile(extractedFile)
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("unexpected extracted content: %s", string(data))
	}

	g.mu.RLock()
	gotVersion := g.managedVersions["frontend"]
	g.mu.RUnlock()
	if gotVersion != "2.0.0" {
		t.Errorf("expected managed version 2.0.0, got %s", gotVersion)
	}
}

func TestUpdateBackend_SignatureFailurePath(t *testing.T) {
	guardPub, _, _ := ed25519.GenerateKey(rand.Reader)
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)

	testBinary := []byte("backend-binary")
	hash := sha256.Sum256(testBinary)
	hashStr := hex.EncodeToString(hash[:])

	digest := sha256.Sum256([]byte(hashStr))
	badSig := ed25519.Sign(otherPriv, digest[:])
	badSigB64 := base64.StdEncoding.EncodeToString(badSig)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/update/download":
			json.NewEncoder(w).Encode(map[string]string{
				"download_url": "/download/backend.bin",
				"sha256":       hashStr,
				"signature":    badSigB64,
			})
		case "/download/backend.bin":
			w.Write(testBinary)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	progressCalled := false
	failureCalled := false
	resultCalled := false

	g := &Guard{
		cfg: Config{
			ServerURL:     server.URL,
			LicenseKey:    "test-key",
			ProjectSlug:   "test-project",
			ComponentSlug: "backend",
			OTA: OTAConfig{
				DownloadTimeout:  5 * time.Second,
				MaxArtifactBytes: 1024 * 1024,
				OnUpdateProgress: func(component, stage string, progress float64) {
					progressCalled = true
				},
				OnUpdateFailure: func(component string, err error) {
					failureCalled = true
				},
				OnUpdateResult: func(component, oldVer, newVer string, success bool, err error) {
					resultCalled = true
				},
			},
		},
		publicKey:   guardPub,
		fingerprint: &Fingerprint{machineID: "test-machine"},
		httpClient:  &http.Client{Timeout: 5 * time.Second},
		updateMu:    sync.Mutex{},
		mu:          sync.RWMutex{},
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	u := updateInfo{Component: "backend", Latest: "2.0.0", UpdateAvailable: true}
	g.updateBackend(u)

	if !progressCalled {
		t.Error("expected progress callback")
	}
	if !failureCalled {
		t.Error("expected failure callback")
	}
	if !resultCalled {
		t.Error("expected result callback")
	}
}

func TestApplyBackendBinaryWithSelfupdate_Success(t *testing.T) {
	// Skip: go-selfupdate.Apply modifies the running binary which cannot work in tests
	t.Skip("go-selfupdate cannot be tested in-process")
}

func TestUpdateFrontend_HashMismatch(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	content := []byte("test")
	hdr := &tar.Header{Name: "test.txt", Mode: 0o644, Size: int64(len(content))}
	tw.WriteHeader(hdr)
	tw.Write(content)
	tw.Close()
	gz.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/update/download":
			json.NewEncoder(w).Encode(map[string]string{
				"download_url": "/download/frontend.tar.gz",
				"sha256":       "badhash",
			})
		case "/download/frontend.tar.gz":
			w.Write(buf.Bytes())
		}
	}))
	defer server.Close()

	failureCalled := false
	tempDir := t.TempDir()

	g := &Guard{
		cfg: Config{
			ServerURL:   server.URL,
			LicenseKey:  "test-key",
			ProjectSlug: "test-project",
			OTA: OTAConfig{
				OnUpdateFailure: func(component string, err error) {
					failureCalled = true
				},
			},
		},
		publicKey:   pubKey,
		fingerprint: &Fingerprint{machineID: "test-machine"},
		httpClient:  &http.Client{Timeout: 5 * time.Second},
		updateMu:    sync.Mutex{},
		mu:          sync.RWMutex{},
		managedVersions: map[string]string{
			"frontend": "1.0.0",
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	mc := ManagedComponent{Slug: "frontend", Dir: tempDir}
	g.updateFrontend(mc, updateInfo{Component: "frontend", Latest: "2.0.0"})

	if !failureCalled {
		t.Error("expected failure callback on hash mismatch")
	}
}

func TestUpdateFrontend_RejectsOversizeArchive(t *testing.T) {
	pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)

	tarGzBytes := buildTarGz(t, map[string]string{
		"frontend.txt": "hello frontend",
	})
	hashStr := sha256Hex(tarGzBytes)
	signature := signUpdateHash(t, privKey, hashStr)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/update/download":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"download_url": "/download/frontend.tar.gz",
				"sha256":       hashStr,
				"signature":    signature,
			})
		case "/download/frontend.tar.gz":
			_, _ = w.Write(tarGzBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var failureErr error
	targetDir := filepath.Join(t.TempDir(), "live")
	g := &Guard{
		cfg: Config{
			ServerURL:     server.URL,
			LicenseKey:    "test-key",
			ProjectSlug:   "test-project",
			ComponentSlug: "backend",
			OTA: OTAConfig{
				MaxArtifactBytes: int64(len(tarGzBytes) - 1),
				OnUpdateFailure: func(component string, err error) {
					failureErr = err
				},
			},
		},
		publicKey:   pubKey,
		fingerprint: &Fingerprint{machineID: "test-machine"},
		httpClient:  &http.Client{Timeout: 5 * time.Second},
		updateMu:    sync.Mutex{},
		mu:          sync.RWMutex{},
		managedVersions: map[string]string{
			"frontend": "1.0.0",
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	err := g.updateFrontend(ManagedComponent{Slug: "frontend", Dir: targetDir}, updateInfo{
		Component:       "frontend",
		Latest:          "2.0.0",
		UpdateAvailable: true,
	})
	if err == nil {
		t.Fatal("expected oversized archive error")
	}
	if !errors.Is(err, ErrUpdateDownload) {
		t.Fatalf("expected ErrUpdateDownload, got %v", err)
	}
	if !errors.Is(failureErr, ErrUpdateDownload) {
		t.Fatalf("expected failure callback ErrUpdateDownload, got %v", failureErr)
	}
	if _, statErr := os.Stat(filepath.Join(targetDir, "frontend.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("oversized archive should not be extracted, statErr=%v", statErr)
	}
	if got := g.currentManagedVersion("frontend"); got != "1.0.0" {
		t.Fatalf("oversized archive should not update version, got %s", got)
	}
}

func TestUpdateFrontend_FailsOnConflictingDirectoryEntry(t *testing.T) {
	pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	content := []byte("not-a-directory")
	if err := tw.WriteHeader(&tar.Header{Name: "conflict", Mode: 0o644, Size: int64(len(content))}); err != nil {
		t.Fatalf("write file header: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("write file body: %v", err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: "conflict", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatalf("write dir header: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}

	tarGzBytes := buf.Bytes()
	hashStr := sha256Hex(tarGzBytes)
	signature := signUpdateHash(t, privKey, hashStr)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/update/download":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"download_url": "/download/frontend.tar.gz",
				"sha256":       hashStr,
				"signature":    signature,
			})
		case "/download/frontend.tar.gz":
			_, _ = w.Write(tarGzBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var failureErr error
	targetDir := filepath.Join(t.TempDir(), "live")
	g := &Guard{
		cfg: Config{
			ServerURL:     server.URL,
			LicenseKey:    "test-key",
			ProjectSlug:   "test-project",
			ComponentSlug: "backend",
			OTA: OTAConfig{
				MaxArtifactBytes: int64(len(tarGzBytes)) + 1024,
				OnUpdateFailure: func(component string, err error) {
					failureErr = err
				},
			},
		},
		publicKey:   pubKey,
		fingerprint: &Fingerprint{machineID: "test-machine"},
		httpClient:  &http.Client{Timeout: 5 * time.Second},
		updateMu:    sync.Mutex{},
		mu:          sync.RWMutex{},
		managedVersions: map[string]string{
			"frontend": "1.0.0",
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	err := g.updateFrontend(ManagedComponent{Slug: "frontend", Dir: targetDir}, updateInfo{
		Component:       "frontend",
		Latest:          "2.0.0",
		UpdateAvailable: true,
	})
	if err == nil {
		t.Fatal("expected conflicting directory entry error")
	}
	if !errors.Is(err, ErrUpdateApply) {
		t.Fatalf("expected ErrUpdateApply, got %v", err)
	}
	if !errors.Is(failureErr, ErrUpdateApply) {
		t.Fatalf("expected failure callback ErrUpdateApply, got %v", failureErr)
	}
	if got := g.currentManagedVersion("frontend"); got != "1.0.0" {
		t.Fatalf("failed archive should not update version, got %s", got)
	}
	if _, statErr := os.Stat(targetDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed archive should not be applied, statErr=%v", statErr)
	}
}

func TestUpdateFrontend_DuplicateFileEntryTruncatesPreviousContent(t *testing.T) {
	pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	first := []byte("first-content-is-long")
	second := []byte("new")
	if err := tw.WriteHeader(&tar.Header{Name: "index.html", Mode: 0o644, Size: int64(len(first))}); err != nil {
		t.Fatalf("write first header: %v", err)
	}
	if _, err := tw.Write(first); err != nil {
		t.Fatalf("write first body: %v", err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: "index.html", Mode: 0o644, Size: int64(len(second))}); err != nil {
		t.Fatalf("write second header: %v", err)
	}
	if _, err := tw.Write(second); err != nil {
		t.Fatalf("write second body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}

	tarGzBytes := buf.Bytes()
	hashStr := sha256Hex(tarGzBytes)
	signature := signUpdateHash(t, privKey, hashStr)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/update/download":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"download_url": "/download/frontend.tar.gz",
				"sha256":       hashStr,
				"signature":    signature,
			})
		case "/download/frontend.tar.gz":
			_, _ = w.Write(tarGzBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	targetDir := filepath.Join(t.TempDir(), "live")
	g := &Guard{
		cfg: Config{
			ServerURL:     server.URL,
			LicenseKey:    "test-key",
			ProjectSlug:   "test-project",
			ComponentSlug: "backend",
			OTA: OTAConfig{
				MaxArtifactBytes: int64(len(tarGzBytes)) + 1024,
			},
		},
		publicKey:   pubKey,
		fingerprint: &Fingerprint{machineID: "test-machine"},
		httpClient:  &http.Client{Timeout: 5 * time.Second},
		updateMu:    sync.Mutex{},
		mu:          sync.RWMutex{},
		managedVersions: map[string]string{
			"frontend": "1.0.0",
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	if err := g.updateFrontend(ManagedComponent{Slug: "frontend", Dir: targetDir}, updateInfo{
		Component:       "frontend",
		Latest:          "2.0.0",
		UpdateAvailable: true,
	}); err != nil {
		t.Fatalf("updateFrontend failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(targetDir, "index.html"))
	if err != nil {
		t.Fatalf("read extracted duplicate file: %v", err)
	}
	if string(data) != string(second) {
		t.Fatalf("duplicate file should be truncated to latest entry, got %q", string(data))
	}
}

func TestUpdateFrontend_PathTraversalBlocked(t *testing.T) {
	pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "../etc/passwd", Mode: 0o644, Size: 5}
	tw.WriteHeader(hdr)
	tw.Write([]byte("root:"))
	tw.Close()
	gz.Close()

	hash := sha256.Sum256(buf.Bytes())
	hashStr := hex.EncodeToString(hash[:])
	signature := signUpdateHash(t, privKey, hashStr)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/update/download":
			json.NewEncoder(w).Encode(map[string]string{
				"download_url": "/download/frontend.tar.gz",
				"sha256":       hashStr,
				"signature":    signature,
			})
		case "/download/frontend.tar.gz":
			w.Write(buf.Bytes())
		}
	}))
	defer server.Close()

	tempDir := t.TempDir()

	g := &Guard{
		cfg: Config{
			ServerURL:   server.URL,
			LicenseKey:  "test-key",
			ProjectSlug: "test-project",
			OTA:         OTAConfig{AutoUpdate: true},
		},
		publicKey:   pubKey,
		fingerprint: &Fingerprint{machineID: "test-machine"},
		httpClient:  &http.Client{Timeout: 5 * time.Second},
		updateMu:    sync.Mutex{},
		mu:          sync.RWMutex{},
		managedVersions: map[string]string{
			"frontend": "1.0.0",
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	mc := ManagedComponent{Slug: "frontend", Dir: tempDir}
	if err := g.updateFrontend(mc, updateInfo{Component: "frontend", Latest: "2.0.0"}); err != nil {
		t.Fatalf("updateFrontend failed: %v", err)
	}

	badPath := filepath.Join(tempDir, "..", "etc", "passwd")
	if _, err := os.Stat(badPath); err == nil {
		t.Error("path traversal should have been blocked")
	}
}
