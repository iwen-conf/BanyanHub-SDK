package sdk

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/creativeprojects/go-selfupdate/update"
)

func (g *Guard) handleUpdateNotification(u updateInfo) {
	// Find matching component config
	if u.Component == g.cfg.ComponentSlug {
		if g.cfg.OTA.AutoUpdate {
			go func() { _ = g.updateBackend(u) }()
		}
		return
	}

	for _, mc := range g.cfg.ManagedComponents {
		if mc.Slug == u.Component {
			if g.cfg.OTA.AutoUpdate {
				// Route based on strategy
				switch mc.Strategy {
				case UpdateBackend:
					go func() { _ = g.updateManagedBackend(mc, u) }()
				case UpdateFrontend:
					go func() { _ = g.updateFrontend(mc, u) }()
				default:
					go func() { _ = g.updateFrontend(mc, u) }()
				}
			}
			return
		}
	}
}

func (g *Guard) updateBackend(u updateInfo) error {
	exe, err := os.Executable()
	if err != nil {
		wrapped := fmt.Errorf("%w: %v", ErrUpdateApply, err)
		g.logger.Error("failed to get executable path", "component", g.cfg.ComponentSlug, "error", err)
		g.notifyUpdateFailure(g.cfg.ComponentSlug, g.currentVersion(), u.Latest, wrapped)
		return wrapped
	}

	return g.updateBinaryComponent(g.cfg.ComponentSlug, u, exe, g.currentVersion, func(newVersion string) {
		g.mu.Lock()
		g.version = newVersion
		g.mu.Unlock()
	})
}

func (g *Guard) updateManagedBackend(mc ManagedComponent, u updateInfo) error {
	targetPath := strings.TrimSpace(mc.Dir)
	if targetPath == "" {
		err := fmt.Errorf("managed backend component %q requires Dir as target binary path", mc.Slug)
		wrapped := fmt.Errorf("%w: %v", ErrUpdateApply, err)
		g.logger.Error("invalid managed backend config", "component", mc.Slug, "error", err)
		g.notifyUpdateFailure(mc.Slug, g.currentManagedVersion(mc.Slug), u.Latest, wrapped)
		return wrapped
	}

	return g.updateBinaryComponent(mc.Slug, u, targetPath, func() string {
		return g.currentManagedVersion(mc.Slug)
	}, func(newVersion string) {
		g.mu.Lock()
		g.managedVersions[mc.Slug] = newVersion
		g.mu.Unlock()
	})
}

func (g *Guard) updateBinaryComponent(
	componentSlug string,
	u updateInfo,
	targetPath string,
	getCurrentVersion func() string,
	setVersion func(newVersion string),
) error {
	if err := g.tryLockUpdate(componentSlug, getCurrentVersion(), u.Latest); err != nil {
		return err
	}
	defer g.updateMu.Unlock()

	oldVersion := getCurrentVersion()
	if !isStrictlyNewerVersion(oldVersion, u.Latest) {
		err := ErrUpdateDowngrade
		g.notifyUpdateFailure(componentSlug, oldVersion, u.Latest, err)
		return err
	}

	g.logger.Info("starting backend update", "component", componentSlug, "old_version", oldVersion, "new_version", u.Latest)

	if g.cfg.OTA.OnUpdateProgress != nil {
		g.cfg.OTA.OnUpdateProgress(componentSlug, "requesting", 0.0)
	}

	// Stage 1: Request download metadata
	osValue, archValue := g.resolveOTAPlatform("", "")
	url, sha256Hash, signature, err := g.requestDownloadMeta(componentSlug, u.Latest, osValue, archValue)
	if err != nil {
		wrapped := fmt.Errorf("%w: %v", ErrUpdateDownload, err)
		g.logger.Error("failed to request download metadata", "component", componentSlug, "error", err.Error())
		g.notifyUpdateFailure(componentSlug, oldVersion, u.Latest, wrapped)
		return wrapped
	}

	if g.cfg.OTA.OnUpdateProgress != nil {
		g.cfg.OTA.OnUpdateProgress(componentSlug, "downloading", 0.3)
	}

	// Stage 2: Download artifact with progress
	tmpPath, actualSHA256, err := g.downloadArtifactWithProgress(url, g.otaMaxArtifactBytes())
	if err != nil {
		wrapped := fmt.Errorf("%w: %v", ErrUpdateDownload, err)
		g.logger.Error("failed to download artifact", "component", componentSlug, "error", err.Error(), "download_url", url)
		g.notifyUpdateFailure(componentSlug, oldVersion, u.Latest, wrapped)
		return wrapped
	}
	defer os.Remove(tmpPath)

	if g.cfg.OTA.OnUpdateProgress != nil {
		g.cfg.OTA.OnUpdateProgress(componentSlug, "verifying", 0.6)
	}

	// Verify SHA256
	if actualSHA256 != sha256Hash {
		err := fmt.Errorf("hash mismatch: expected %s, got %s", sha256Hash, actualSHA256)
		wrapped := fmt.Errorf("%w: %v", ErrUpdateVerify, err)
		g.logger.Error("hash verification failed", "component", componentSlug, "error", err)
		g.notifyUpdateFailure(componentSlug, oldVersion, u.Latest, wrapped)
		return wrapped
	}

	// Verify signature
	if err := g.verifySignature(sha256Hash, signature); err != nil {
		wrapped := fmt.Errorf("%w: %v", ErrUpdateVerify, err)
		g.logger.Error("signature verification failed", "component", componentSlug, "error", err)
		g.notifyUpdateFailure(componentSlug, oldVersion, u.Latest, wrapped)
		return wrapped
	}

	if g.cfg.OTA.OnUpdateProgress != nil {
		g.cfg.OTA.OnUpdateProgress(componentSlug, "applying", 0.8)
	}

	// Stage 3: Apply binary update using go-selfupdate
	if err := g.applyBackendBinaryWithSelfupdate(tmpPath, targetPath); err != nil {
		wrapped := fmt.Errorf("%w: %v", ErrUpdateApply, err)
		g.logger.Error("failed to apply update", "component", componentSlug, "error", err)
		g.notifyUpdateFailure(componentSlug, oldVersion, u.Latest, wrapped)
		return wrapped
	}

	setVersion(u.Latest)

	g.logger.Info("backend update completed", "component", componentSlug, "old_version", oldVersion, "new_version", u.Latest)

	if g.cfg.OTA.OnUpdateResult != nil {
		g.cfg.OTA.OnUpdateResult(componentSlug, oldVersion, u.Latest, true, nil)
	}

	if g.cfg.OTA.OnUpdateProgress != nil {
		g.cfg.OTA.OnUpdateProgress(componentSlug, "completed", 1.0)
	}

	return nil
}

func (g *Guard) currentVersion() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.version
}

func (g *Guard) currentManagedVersion(slug string) string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.managedVersions[slug]
}

func (g *Guard) requestDownloadMeta(component, version, os, arch string) (url, sha256, signature string, err error) {
	reqBody := map[string]any{
		"license_key":    g.cfg.LicenseKey,
		"machine_id":     g.fingerprint.MachineID(),
		"project_slug":   g.cfg.ProjectSlug,
		"component_slug": component,
		"version":        version,
		"os":             os,
		"arch":           arch,
	}

	var resp struct {
		DownloadURL string `json:"download_url"`
		SHA256      string `json:"sha256"`
		Signature   string `json:"signature"`
		Error       string `json:"error"`
	}

	ctx, cancel := context.WithTimeout(context.Background(), g.otaDownloadTimeout())
	defer cancel()

	if err := g.postJSON(ctx, "/api/v1/update/download", reqBody, &resp); err != nil {
		return "", "", "", err
	}

	if resp.Error != "" {
		return "", "", "", fmt.Errorf("server error: %s", resp.Error)
	}

	return resp.DownloadURL, resp.SHA256, resp.Signature, nil
}

func (g *Guard) downloadArtifactWithProgress(downloadURL string, maxBytes int64) (tmpPath, sha256Hash string, err error) {
	fullURL := g.cfg.ServerURL + downloadURL

	ctx, cancel := context.WithTimeout(context.Background(), g.otaDownloadTimeout())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "BanyanHub-SDK/"+Version)

	httpResp, err := g.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("download failed: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("download failed with status %d", httpResp.StatusCode)
	}

	tmpFile, err := os.CreateTemp("", "deploy-guard-update-*")
	if err != nil {
		return "", "", fmt.Errorf("create temp file: %w", err)
	}
	defer tmpFile.Close()

	hasher := sha256.New()
	limitedReader := io.LimitReader(httpResp.Body, maxBytes)

	if _, err := io.Copy(io.MultiWriter(tmpFile, hasher), limitedReader); err != nil {
		os.Remove(tmpFile.Name())
		return "", "", fmt.Errorf("copy failed: %w", err)
	}

	actualHash := hex.EncodeToString(hasher.Sum(nil))
	return tmpFile.Name(), actualHash, nil
}

func (g *Guard) verifySignature(data, signatureB64 string) error {
	return verifyEd25519Digest([]byte(data), signatureB64, g.verificationKeys())
}

func (g *Guard) applyBackendBinaryWithSelfupdate(tmpPath, targetPath string) error {
	tmpFile, err := os.Open(tmpPath)
	if err != nil {
		return fmt.Errorf("open temp file: %w", err)
	}
	defer tmpFile.Close()

	opts := update.Options{
		TargetPath:  targetPath,
		OldSavePath: targetPath + ".bak",
	}

	if err := update.Apply(tmpFile, opts); err != nil {
		if rerr := update.RollbackError(err); rerr != nil {
			return fmt.Errorf("%w: rollback also failed: %v", ErrUpdateRollback, rerr)
		}
		return err
	}

	return nil
}

func (g *Guard) updateFrontend(mc ManagedComponent, u updateInfo) error {
	oldVersion := g.currentManagedVersion(mc.Slug)
	if err := g.tryLockUpdate(mc.Slug, oldVersion, u.Latest); err != nil {
		return err
	}
	defer g.updateMu.Unlock()

	g.logger.Info("starting frontend update", "component", mc.Slug, "version", u.Latest)

	if !isStrictlyNewerVersion(oldVersion, u.Latest) {
		g.notifyUpdateFailure(mc.Slug, oldVersion, u.Latest, ErrUpdateDowngrade)
		return ErrUpdateDowngrade
	}

	if g.cfg.OTA.OnUpdateProgress != nil {
		g.cfg.OTA.OnUpdateProgress(mc.Slug, "requesting", 0.0)
	}

	osValue, archValue := g.resolveOTAPlatform("", "")
	downloadURL, expectedSHA256, signature, err := g.requestDownloadMeta(mc.Slug, u.Latest, osValue, archValue)
	if err != nil {
		wrapped := fmt.Errorf("%w: %v", ErrUpdateDownload, err)
		g.logger.Error("failed to request download", "component", mc.Slug, "error", err)
		g.notifyUpdateFailure(mc.Slug, oldVersion, u.Latest, wrapped)
		return wrapped
	}

	if g.cfg.OTA.OnUpdateProgress != nil {
		g.cfg.OTA.OnUpdateProgress(mc.Slug, "downloading", 0.3)
	}

	ctx, cancel := context.WithTimeout(context.Background(), g.otaDownloadTimeout())
	defer cancel()

	// Download tar.gz
	fullURL := g.cfg.ServerURL + downloadURL
	dlReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		wrapped := fmt.Errorf("%w: %v", ErrUpdateDownload, err)
		g.logger.Error("failed to create download request", "component", mc.Slug, "error", err)
		g.notifyUpdateFailure(mc.Slug, oldVersion, u.Latest, wrapped)
		return wrapped
	}
	dlReq.Header.Set("User-Agent", "BanyanHub-SDK/"+Version)
	httpResp, err := g.httpClient.Do(dlReq)
	if err != nil {
		wrapped := fmt.Errorf("%w: %v", ErrUpdateDownload, err)
		g.logger.Error("failed to download", "component", mc.Slug, "error", err)
		g.notifyUpdateFailure(mc.Slug, oldVersion, u.Latest, wrapped)
		return wrapped
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		wrapped := fmt.Errorf("%w: status %d", ErrUpdateDownload, httpResp.StatusCode)
		g.logger.Error("download failed with status", "component", mc.Slug, "status", httpResp.StatusCode)
		g.notifyUpdateFailure(mc.Slug, oldVersion, u.Latest, wrapped)
		return wrapped
	}

	tmpDir, err := os.MkdirTemp("", "deploy-guard-frontend-*")
	if err != nil {
		wrapped := fmt.Errorf("%w: %v", ErrUpdateApply, err)
		g.logger.Error("failed to create temp dir", "component", mc.Slug, "error", err)
		g.notifyUpdateFailure(mc.Slug, oldVersion, u.Latest, wrapped)
		return wrapped
	}
	defer os.RemoveAll(tmpDir)

	if g.cfg.OTA.OnUpdateProgress != nil {
		g.cfg.OTA.OnUpdateProgress(mc.Slug, "extracting", 0.5)
	}

	// Stream through SHA256 hasher → gzip → tar extraction with size limit
	hasher := sha256.New()
	limitedReader := io.LimitReader(httpResp.Body, g.otaMaxArtifactBytes())
	tee := io.TeeReader(limitedReader, hasher)

	gz, err := gzip.NewReader(tee)
	if err != nil {
		wrapped := fmt.Errorf("%w: %v", ErrUpdateVerify, err)
		g.logger.Error("failed to create gzip reader", "component", mc.Slug, "error", err)
		g.notifyUpdateFailure(mc.Slug, oldVersion, u.Latest, wrapped)
		return wrapped
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			wrapped := fmt.Errorf("%w: %v", ErrUpdateVerify, err)
			g.logger.Error("failed to read tar entry", "component", mc.Slug, "error", err)
			g.notifyUpdateFailure(mc.Slug, oldVersion, u.Latest, wrapped)
			return wrapped
		}

		target := filepath.Join(tmpDir, hdr.Name)
		cleanedTarget := filepath.Clean(target)
		cleanedTmpDir := filepath.Clean(tmpDir) + string(os.PathSeparator)
		if !strings.HasPrefix(cleanedTarget, cleanedTmpDir) {
			g.logger.Warn("path traversal attempt detected", "component", mc.Slug, "path", hdr.Name)
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, os.FileMode(hdr.Mode))
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(target), 0o755)
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, os.FileMode(hdr.Mode))
			if err != nil {
				wrapped := fmt.Errorf("%w: %v", ErrUpdateApply, err)
				g.logger.Error("failed to create file", "component", mc.Slug, "file", target, "error", err)
				g.notifyUpdateFailure(mc.Slug, oldVersion, u.Latest, wrapped)
				return wrapped
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				wrapped := fmt.Errorf("%w: %v", ErrUpdateApply, err)
				g.logger.Error("failed to write file", "component", mc.Slug, "file", target, "error", err)
				g.notifyUpdateFailure(mc.Slug, oldVersion, u.Latest, wrapped)
				return wrapped
			}
			f.Close()
		}
	}

	if g.cfg.OTA.OnUpdateProgress != nil {
		g.cfg.OTA.OnUpdateProgress(mc.Slug, "verifying", 0.8)
	}

	// Verify SHA256
	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if actualHash != expectedSHA256 {
		wrapped := fmt.Errorf("%w: hash mismatch", ErrUpdateVerify)
		g.logger.Error("hash mismatch", "component", mc.Slug, "expected", expectedSHA256, "actual", actualHash)
		g.notifyUpdateFailure(mc.Slug, oldVersion, u.Latest, wrapped)
		return wrapped
	}
	if err := g.verifySignature(expectedSHA256, signature); err != nil {
		wrapped := fmt.Errorf("%w: %v", ErrUpdateVerify, err)
		g.logger.Error("signature verification failed", "component", mc.Slug, "error", err)
		g.notifyUpdateFailure(mc.Slug, oldVersion, u.Latest, wrapped)
		return wrapped
	}

	if g.cfg.OTA.OnUpdateProgress != nil {
		g.cfg.OTA.OnUpdateProgress(mc.Slug, "applying", 0.9)
	}

	// Atomic swap: old → .bak, new → target
	backupDir := mc.Dir + ".bak"
	os.RemoveAll(backupDir)

	if _, err := os.Stat(mc.Dir); err == nil {
		if err := os.Rename(mc.Dir, backupDir); err != nil {
			wrapped := fmt.Errorf("%w: %v", ErrUpdateApply, err)
			g.logger.Error("failed to backup old dir", "component", mc.Slug, "error", err)
			g.notifyUpdateFailure(mc.Slug, oldVersion, u.Latest, wrapped)
			return wrapped
		}
	}

	if err := os.Rename(tmpDir, mc.Dir); err != nil {
		os.Rename(backupDir, mc.Dir) // rollback
		wrapped := fmt.Errorf("%w: %v", ErrUpdateApply, err)
		g.logger.Error("failed to move new dir", "component", mc.Slug, "error", err)
		g.notifyUpdateFailure(mc.Slug, oldVersion, u.Latest, wrapped)
		return wrapped
	}

	// Update version under lock
	g.mu.Lock()
	g.managedVersions[mc.Slug] = u.Latest
	g.mu.Unlock()

	g.logger.Info("frontend update completed", "component", mc.Slug, "old_version", oldVersion, "new_version", u.Latest)

	if g.cfg.OTA.OnUpdateResult != nil {
		g.cfg.OTA.OnUpdateResult(mc.Slug, oldVersion, u.Latest, true, nil)
	}

	if g.cfg.OTA.OnUpdateProgress != nil {
		g.cfg.OTA.OnUpdateProgress(mc.Slug, "completed", 1.0)
	}

	// Post-update hook
	if mc.PostUpdate != nil {
		if err := mc.PostUpdate(); err != nil {
			g.logger.Error("post update hook failed", "component", mc.Slug, "error", err)
		}
	}

	return nil
}

func isStrictlyNewerVersion(current, target string) bool {
	currentVersion, currentErr := semver.NewVersion(strings.TrimSpace(strings.TrimPrefix(current, "v")))
	targetVersion, targetErr := semver.NewVersion(strings.TrimSpace(strings.TrimPrefix(target, "v")))
	if currentErr != nil || targetErr != nil {
		return target != "" && target != current
	}
	return targetVersion.GreaterThan(currentVersion)
}

func (g *Guard) tryLockUpdate(component, oldVersion, newVersion string) error {
	if g.updateMu.TryLock() {
		return nil
	}

	g.notifyUpdateFailure(component, oldVersion, newVersion, ErrUpdateConcurrent)
	return ErrUpdateConcurrent
}

func (g *Guard) notifyUpdateFailure(component, oldVersion, newVersion string, err error) {
	if g.cfg.OTA.OnUpdateFailure != nil {
		g.cfg.OTA.OnUpdateFailure(component, err)
	}
	if g.cfg.OTA.OnUpdateResult != nil {
		g.cfg.OTA.OnUpdateResult(component, oldVersion, newVersion, false, err)
	}
}

func (g *Guard) otaDownloadTimeout() time.Duration {
	if g.cfg.OTA.DownloadTimeout > 0 {
		return g.cfg.OTA.DownloadTimeout
	}
	return 10 * time.Minute
}

func (g *Guard) otaMaxArtifactBytes() int64 {
	if g.cfg.OTA.MaxArtifactBytes > 0 {
		return g.cfg.OTA.MaxArtifactBytes
	}
	return 500 * 1024 * 1024
}
