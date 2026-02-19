package sdk

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
				go g.updateFrontend(mc, u)
			}
			return
		}
	}
}

func (g *Guard) updateBackend(u updateInfo) {
	// Request download URL from server
	reqBody := map[string]any{
		"license_key":    g.cfg.LicenseKey,
		"machine_id":     g.fingerprint.MachineID(),
		"project_slug":   g.cfg.ProjectSlug,
		"component_slug": g.cfg.ComponentSlug,
		"version":        u.Latest,
		"platform":       g.cfg.OTA.Platform,
	}

	var resp struct {
		DownloadURL string `json:"download_url"`
		SHA256      string `json:"sha256"`
		Signature   string `json:"signature"`
		Error       string `json:"error"`
	}

	if err := g.postJSON("/api/v1/update/download", reqBody, &resp); err != nil {
		return
	}
	if resp.Error != "" {
		return
	}

	// Download binary to temp file
	tmpFile, err := os.CreateTemp("", "deploy-guard-update-*")
	if err != nil {
		return
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	fullURL := g.cfg.ServerURL + resp.DownloadURL
	httpResp, err := http.Get(fullURL)
	if err != nil {
		return
	}
	defer httpResp.Body.Close()

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmpFile, hasher), httpResp.Body); err != nil {
		return
	}

	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if actualHash != resp.SHA256 {
		return // hash mismatch
	}

	// Binary self-replacement via go-selfupdate would go here
	// For MVP, we write the binary and signal for restart
	tmpFile.Close()

	exe, err := os.Executable()
	if err != nil {
		return
	}

	backup := exe + ".bak"
	os.Remove(backup)
	if err := os.Rename(exe, backup); err != nil {
		return
	}

	if err := copyFile(tmpFile.Name(), exe); err != nil {
		os.Rename(backup, exe) // rollback
		return
	}

	os.Chmod(exe, 0o755)
	g.version = u.Latest
}

func (g *Guard) updateFrontend(mc ManagedComponent, u updateInfo) {
	reqBody := map[string]any{
		"license_key":    g.cfg.LicenseKey,
		"machine_id":     g.fingerprint.MachineID(),
		"project_slug":   g.cfg.ProjectSlug,
		"component_slug": mc.Slug,
		"version":        u.Latest,
		"platform":       "universal",
	}

	var resp struct {
		DownloadURL string `json:"download_url"`
		SHA256      string `json:"sha256"`
		Error       string `json:"error"`
	}

	if err := g.postJSON("/api/v1/update/download", reqBody, &resp); err != nil {
		return
	}
	if resp.Error != "" {
		return
	}

	// Download tar.gz
	fullURL := g.cfg.ServerURL + resp.DownloadURL
	httpResp, err := http.Get(fullURL)
	if err != nil {
		return
	}
	defer httpResp.Body.Close()

	tmpDir, err := os.MkdirTemp("", "deploy-guard-frontend-*")
	if err != nil {
		return
	}
	defer os.RemoveAll(tmpDir)

	// Stream through SHA256 hasher → gzip → tar extraction
	hasher := sha256.New()
	tee := io.TeeReader(httpResp.Body, hasher)

	gz, err := gzip.NewReader(tee)
	if err != nil {
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
			return
		}

		target := filepath.Join(tmpDir, hdr.Name)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(tmpDir)) {
			continue // path traversal protection
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, os.FileMode(hdr.Mode))
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(target), 0o755)
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, os.FileMode(hdr.Mode))
			if err != nil {
				return
			}
			io.Copy(f, tr)
			f.Close()
		}
	}

	// Verify SHA256
	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if actualHash != resp.SHA256 {
		return
	}

	// Atomic swap: old → .bak, new → target
	backupDir := mc.Dir + ".bak"
	os.RemoveAll(backupDir)

	if _, err := os.Stat(mc.Dir); err == nil {
		if err := os.Rename(mc.Dir, backupDir); err != nil {
			return
		}
	}

	if err := os.Rename(tmpDir, mc.Dir); err != nil {
		os.Rename(backupDir, mc.Dir) // rollback
		return
	}

	g.managedVersions[mc.Slug] = u.Latest

	// Post-update hook
	if mc.PostUpdate != nil {
		if err := mc.PostUpdate(); err != nil {
			// Log but don't rollback — files are already swapped
			_ = err
		}
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
