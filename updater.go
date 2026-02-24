package sdk

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/creativeprojects/go-selfupdate/update"
)

func (g *Guard) handleUpdateNotification(u updateInfo) {
	// Find matching component config
	if u.Component == g.cfg.ComponentSlug {
		if g.cfg.OTA.AutoUpdate {
			go g.updateBackend(u)
		}
		return
	}

	for _, mc := range g.cfg.ManagedComponents {
		if mc.Slug == u.Component {
			if g.cfg.OTA.AutoUpdate {
				// Route based on strategy
				switch mc.Strategy {
				case UpdateBackend:
					go g.updateManagedBackend(mc, u)
				case UpdateFrontend:
					go g.updateFrontend(mc, u)
				default:
					go g.updateFrontend(mc, u)
				}
			}
			return
		}
	}
}

func (g *Guard) updateBackend(u updateInfo) {
	exe, err := os.Executable()
	if err != nil {
		g.logger.Error("failed to get executable path", "component", g.cfg.ComponentSlug, "error", err)
		if g.cfg.OTA.OnUpdateFailure != nil {
			g.cfg.OTA.OnUpdateFailure(g.cfg.ComponentSlug, fmt.Errorf("%w: %v", ErrUpdateApply, err))
		}
		if g.cfg.OTA.OnUpdateResult != nil {
			g.cfg.OTA.OnUpdateResult(g.cfg.ComponentSlug, g.currentVersion(), u.Latest, false, err)
		}
		return
	}

	g.updateBinaryComponent(g.cfg.ComponentSlug, u, exe, g.currentVersion, func(newVersion string) {
		g.mu.Lock()
		g.version = newVersion
		g.mu.Unlock()
	})
}

func (g *Guard) updateManagedBackend(mc ManagedComponent, u updateInfo) {
	targetPath := strings.TrimSpace(mc.Dir)
	if targetPath == "" {
		err := fmt.Errorf("managed backend component %q requires Dir as target binary path", mc.Slug)
		g.logger.Error("invalid managed backend config", "component", mc.Slug, "error", err)
		if g.cfg.OTA.OnUpdateFailure != nil {
			g.cfg.OTA.OnUpdateFailure(mc.Slug, fmt.Errorf("%w: %v", ErrUpdateApply, err))
		}
		if g.cfg.OTA.OnUpdateResult != nil {
			g.cfg.OTA.OnUpdateResult(mc.Slug, g.currentManagedVersion(mc.Slug), u.Latest, false, err)
		}
		return
	}

	g.updateBinaryComponent(mc.Slug, u, targetPath, func() string {
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
) {
	// Acquire update lock to prevent concurrent updates
	g.updateMu.Lock()
	defer g.updateMu.Unlock()

	oldVersion := getCurrentVersion()

	g.logger.Info("starting backend update", "component", componentSlug, "old_version", oldVersion, "new_version", u.Latest)

	if g.cfg.OTA.OnUpdateProgress != nil {
		g.cfg.OTA.OnUpdateProgress(componentSlug, "requesting", 0.0)
	}

	// Stage 1: Request download metadata
	url, sha256Hash, signature, err := g.requestDownloadMeta(componentSlug, u.Latest, g.cfg.OTA.OS, g.cfg.OTA.Arch)
	if err != nil {
		g.logger.Error("failed to request download metadata", "component", componentSlug, "error", err)
		if g.cfg.OTA.OnUpdateFailure != nil {
			g.cfg.OTA.OnUpdateFailure(componentSlug, fmt.Errorf("%w: %v", ErrUpdateDownload, err))
		}
		if g.cfg.OTA.OnUpdateResult != nil {
			g.cfg.OTA.OnUpdateResult(componentSlug, oldVersion, u.Latest, false, err)
		}
		return
	}

	if g.cfg.OTA.OnUpdateProgress != nil {
		g.cfg.OTA.OnUpdateProgress(componentSlug, "downloading", 0.3)
	}

	// Stage 2: Download artifact with progress
	tmpPath, actualSHA256, err := g.downloadArtifactWithProgress(url, g.cfg.OTA.MaxArtifactBytes)
	if err != nil {
		g.logger.Error("failed to download artifact", "component", componentSlug, "error", err)
		if g.cfg.OTA.OnUpdateFailure != nil {
			g.cfg.OTA.OnUpdateFailure(componentSlug, fmt.Errorf("%w: %v", ErrUpdateDownload, err))
		}
		if g.cfg.OTA.OnUpdateResult != nil {
			g.cfg.OTA.OnUpdateResult(componentSlug, oldVersion, u.Latest, false, err)
		}
		return
	}
	defer os.Remove(tmpPath)

	if g.cfg.OTA.OnUpdateProgress != nil {
		g.cfg.OTA.OnUpdateProgress(componentSlug, "verifying", 0.6)
	}

	// Verify SHA256
	if actualSHA256 != sha256Hash {
		err := fmt.Errorf("hash mismatch: expected %s, got %s", sha256Hash, actualSHA256)
		g.logger.Error("hash verification failed", "component", componentSlug, "error", err)
		if g.cfg.OTA.OnUpdateFailure != nil {
			g.cfg.OTA.OnUpdateFailure(componentSlug, fmt.Errorf("%w: %v", ErrUpdateVerify, err))
		}
		if g.cfg.OTA.OnUpdateResult != nil {
			g.cfg.OTA.OnUpdateResult(componentSlug, oldVersion, u.Latest, false, err)
		}
		return
	}

	// Verify signature
	if err := g.verifySignature(sha256Hash, signature); err != nil {
		g.logger.Error("signature verification failed", "component", componentSlug, "error", err)
		if g.cfg.OTA.OnUpdateFailure != nil {
			g.cfg.OTA.OnUpdateFailure(componentSlug, fmt.Errorf("%w: %v", ErrUpdateVerify, err))
		}
		if g.cfg.OTA.OnUpdateResult != nil {
			g.cfg.OTA.OnUpdateResult(componentSlug, oldVersion, u.Latest, false, err)
		}
		return
	}

	if g.cfg.OTA.OnUpdateProgress != nil {
		g.cfg.OTA.OnUpdateProgress(componentSlug, "applying", 0.8)
	}

	// Stage 3: Apply binary update using go-selfupdate
	if err := g.applyBackendBinaryWithSelfupdate(tmpPath, targetPath); err != nil {
		g.logger.Error("failed to apply update", "component", componentSlug, "error", err)
		if g.cfg.OTA.OnUpdateFailure != nil {
			g.cfg.OTA.OnUpdateFailure(componentSlug, fmt.Errorf("%w: %v", ErrUpdateApply, err))
		}
		if g.cfg.OTA.OnUpdateResult != nil {
			g.cfg.OTA.OnUpdateResult(componentSlug, oldVersion, u.Latest, false, err)
		}
		return
	}

	setVersion(u.Latest)

	g.logger.Info("backend update completed", "component", componentSlug, "old_version", oldVersion, "new_version", u.Latest)

	if g.cfg.OTA.OnUpdateResult != nil {
		g.cfg.OTA.OnUpdateResult(componentSlug, oldVersion, u.Latest, true, nil)
	}

	if g.cfg.OTA.OnUpdateProgress != nil {
		g.cfg.OTA.OnUpdateProgress(componentSlug, "completed", 1.0)
	}
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

	ctx, cancel := context.WithTimeout(context.Background(), g.cfg.OTA.DownloadTimeout)
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

	ctx, cancel := context.WithTimeout(context.Background(), g.cfg.OTA.DownloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("create request: %w", err)
	}

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
	sig, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}

	digest := sha256.Sum256([]byte(data))
	if !ed25519.Verify(g.publicKey, digest[:], sig) {
		return fmt.Errorf("signature verification failed")
	}

	return nil
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

func (g *Guard) updateFrontend(mc ManagedComponent, u updateInfo) {
	// Acquire update lock
	g.updateMu.Lock()
	defer g.updateMu.Unlock()

	g.logger.Info("starting frontend update", "component", mc.Slug, "version", u.Latest)

	if g.cfg.OTA.OnUpdateProgress != nil {
		g.cfg.OTA.OnUpdateProgress(mc.Slug, "requesting", 0.0)
	}

	reqBody := map[string]any{
		"license_key":    g.cfg.LicenseKey,
		"machine_id":     g.fingerprint.MachineID(),
		"project_slug":   g.cfg.ProjectSlug,
		"component_slug": mc.Slug,
		"version":        u.Latest,
		"os":             "universal",
		"arch":           "universal",
	}

	var resp struct {
		DownloadURL string `json:"download_url"`
		SHA256      string `json:"sha256"`
		Error       string `json:"error"`
	}

	if err := g.postJSON(context.Background(), "/api/v1/update/download", reqBody, &resp); err != nil {
		g.logger.Error("failed to request download", "component", mc.Slug, "error", err)
		if g.cfg.OTA.OnUpdateFailure != nil {
			g.cfg.OTA.OnUpdateFailure(mc.Slug, fmt.Errorf("%w: %v", ErrUpdateDownload, err))
		}
		return
	}
	if resp.Error != "" {
		g.logger.Error("server returned error", "component", mc.Slug, "error", resp.Error)
		if g.cfg.OTA.OnUpdateFailure != nil {
			g.cfg.OTA.OnUpdateFailure(mc.Slug, fmt.Errorf("%w: %s", ErrUpdateDownload, resp.Error))
		}
		return
	}

	if g.cfg.OTA.OnUpdateProgress != nil {
		g.cfg.OTA.OnUpdateProgress(mc.Slug, "downloading", 0.3)
	}

	// Download tar.gz
	fullURL := g.cfg.ServerURL + resp.DownloadURL
	httpResp, err := http.Get(fullURL)
	if err != nil {
		g.logger.Error("failed to download", "component", mc.Slug, "error", err)
		if g.cfg.OTA.OnUpdateFailure != nil {
			g.cfg.OTA.OnUpdateFailure(mc.Slug, fmt.Errorf("%w: %v", ErrUpdateDownload, err))
		}
		return
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		g.logger.Error("download failed with status", "component", mc.Slug, "status", httpResp.StatusCode)
		if g.cfg.OTA.OnUpdateFailure != nil {
			g.cfg.OTA.OnUpdateFailure(mc.Slug, fmt.Errorf("%w: status %d", ErrUpdateDownload, httpResp.StatusCode))
		}
		return
	}

	tmpDir, err := os.MkdirTemp("", "deploy-guard-frontend-*")
	if err != nil {
		g.logger.Error("failed to create temp dir", "component", mc.Slug, "error", err)
		if g.cfg.OTA.OnUpdateFailure != nil {
			g.cfg.OTA.OnUpdateFailure(mc.Slug, fmt.Errorf("%w: %v", ErrUpdateApply, err))
		}
		return
	}
	defer os.RemoveAll(tmpDir)

	if g.cfg.OTA.OnUpdateProgress != nil {
		g.cfg.OTA.OnUpdateProgress(mc.Slug, "extracting", 0.5)
	}

	// Stream through SHA256 hasher → gzip → tar extraction with size limit
	hasher := sha256.New()
	limitedReader := io.LimitReader(httpResp.Body, g.cfg.OTA.MaxArtifactBytes)
	tee := io.TeeReader(limitedReader, hasher)

	gz, err := gzip.NewReader(tee)
	if err != nil {
		g.logger.Error("failed to create gzip reader", "component", mc.Slug, "error", err)
		if g.cfg.OTA.OnUpdateFailure != nil {
			g.cfg.OTA.OnUpdateFailure(mc.Slug, fmt.Errorf("%w: %v", ErrUpdateVerify, err))
		}
		return
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			g.logger.Error("failed to read tar entry", "component", mc.Slug, "error", err)
			if g.cfg.OTA.OnUpdateFailure != nil {
				g.cfg.OTA.OnUpdateFailure(mc.Slug, fmt.Errorf("%w: %v", ErrUpdateVerify, err))
			}
			return
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
				g.logger.Error("failed to create file", "component", mc.Slug, "file", target, "error", err)
				if g.cfg.OTA.OnUpdateFailure != nil {
					g.cfg.OTA.OnUpdateFailure(mc.Slug, fmt.Errorf("%w: %v", ErrUpdateApply, err))
				}
				return
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				g.logger.Error("failed to write file", "component", mc.Slug, "file", target, "error", err)
				if g.cfg.OTA.OnUpdateFailure != nil {
					g.cfg.OTA.OnUpdateFailure(mc.Slug, fmt.Errorf("%w: %v", ErrUpdateApply, err))
				}
				return
			}
			f.Close()
		}
	}

	if g.cfg.OTA.OnUpdateProgress != nil {
		g.cfg.OTA.OnUpdateProgress(mc.Slug, "verifying", 0.8)
	}

	// Verify SHA256
	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if actualHash != resp.SHA256 {
		g.logger.Error("hash mismatch", "component", mc.Slug, "expected", resp.SHA256, "actual", actualHash)
		if g.cfg.OTA.OnUpdateFailure != nil {
			g.cfg.OTA.OnUpdateFailure(mc.Slug, fmt.Errorf("%w: hash mismatch", ErrUpdateVerify))
		}
		return
	}

	if g.cfg.OTA.OnUpdateProgress != nil {
		g.cfg.OTA.OnUpdateProgress(mc.Slug, "applying", 0.9)
	}

	// Atomic swap: old → .bak, new → target
	backupDir := mc.Dir + ".bak"
	os.RemoveAll(backupDir)

	if _, err := os.Stat(mc.Dir); err == nil {
		if err := os.Rename(mc.Dir, backupDir); err != nil {
			g.logger.Error("failed to backup old dir", "component", mc.Slug, "error", err)
			if g.cfg.OTA.OnUpdateFailure != nil {
				g.cfg.OTA.OnUpdateFailure(mc.Slug, fmt.Errorf("%w: %v", ErrUpdateApply, err))
			}
			return
		}
	}

	if err := os.Rename(tmpDir, mc.Dir); err != nil {
		os.Rename(backupDir, mc.Dir) // rollback
		g.logger.Error("failed to move new dir", "component", mc.Slug, "error", err)
		if g.cfg.OTA.OnUpdateFailure != nil {
			g.cfg.OTA.OnUpdateFailure(mc.Slug, fmt.Errorf("%w: %v", ErrUpdateApply, err))
		}
		return
	}

	// Update version under lock
	g.mu.Lock()
	oldVersion := g.managedVersions[mc.Slug]
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
			// Log but don't rollback — files are already swapped
			_ = err
		}
	}
}
