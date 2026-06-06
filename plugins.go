package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

type PluginInfo struct {
	Slug             string  `json:"slug"`
	Name             string  `json:"name"`
	Type             string  `json:"type"`
	Description      *string `json:"description"`
	OTAEnabled       bool    `json:"ota_enabled"`
	InstalledVersion *string `json:"installed_version"`
	LatestVersion    *string `json:"latest_version"`
	UpdateAvailable  bool    `json:"update_available"`
	CanUpdate        bool    `json:"can_update"`
	ReleaseNotes     *string `json:"release_notes"`
	SizeBytes        *int64  `json:"size_bytes"`
	TargetOS         *string `json:"target_os"`
	TargetArch       *string `json:"target_arch"`
}

type PluginCatalog struct {
	ProjectSlug  string       `json:"project_slug"`
	MachineID    string       `json:"machine_id"`
	SourceOS     string       `json:"source_os"`
	SourceArch   string       `json:"source_arch"`
	UpdateFrozen bool         `json:"update_frozen"`
	Plugins      []PluginInfo `json:"plugins"`
}

type PluginUpdateOptions struct {
	Version string
	OS      string
	Arch    string
}

type PluginUpdatePackage struct {
	Message         string  `json:"message"`
	Plugin          string  `json:"plugin"`
	CurrentVersion  *string `json:"current_version"`
	TargetVersion   string  `json:"target_version"`
	UpdateAvailable bool    `json:"update_available"`
	DownloadURL     string  `json:"download_url"`
	SHA256          string  `json:"sha256"`
	Signature       string  `json:"signature"`
	SizeBytes       int64   `json:"size_bytes"`
	ReleaseNotes    *string `json:"release_notes"`
	ExpiresIn       int     `json:"expires_in"`
}

type pluginUpdateRequestBody struct {
	LicenseKey  string `json:"license_key"`
	MachineID   string `json:"machine_id"`
	ProjectSlug string `json:"project_slug"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	Version     string `json:"version,omitempty"`
}

// GetPluginCatalog fetches discoverable plugins and update availability for this machine.
func (g *Guard) GetPluginCatalog(ctx context.Context, includeUninstalled bool) (*PluginCatalog, error) {
	query := url.Values{}
	query.Set("license_key", g.cfg.LicenseKey)
	query.Set("machine_id", g.fingerprint.MachineID())
	query.Set("project_slug", g.cfg.ProjectSlug)
	query.Set("os", g.cfg.OTA.OS)
	query.Set("arch", g.cfg.OTA.Arch)
	if !includeUninstalled {
		query.Set("include_uninstalled", "false")
	}

	var resp PluginCatalog
	raw, err := g.getJSON(ctx, "/api/v1/plugins/catalog", query)
	if err != nil {
		return nil, fmt.Errorf("request plugin catalog: %w", err)
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidServerResponse, err)
	}

	return &resp, nil
}

// ListPlugins lists all discoverable plugins for this machine.
func (g *Guard) ListPlugins(ctx context.Context) ([]PluginInfo, error) {
	catalog, err := g.GetPluginCatalog(ctx, true)
	if err != nil {
		return nil, err
	}
	return catalog.Plugins, nil
}

// CheckPluginUpdates returns only plugins with available updates.
func (g *Guard) CheckPluginUpdates(ctx context.Context) ([]PluginInfo, error) {
	plugins, err := g.ListPlugins(ctx)
	if err != nil {
		return nil, err
	}

	updates := make([]PluginInfo, 0, len(plugins))
	for _, plugin := range plugins {
		if plugin.UpdateAvailable {
			updates = append(updates, plugin)
		}
	}
	return updates, nil
}

// RequestPluginUpdate asks the server for a short-lived download package for one plugin.
func (g *Guard) RequestPluginUpdate(ctx context.Context, slug string, options PluginUpdateOptions) (*PluginUpdatePackage, error) {
	if slug == "" {
		return nil, fmt.Errorf("plugin slug is required")
	}

	osValue, archValue := g.resolveOTAPlatform(options.OS, options.Arch)
	body := pluginUpdateRequestBody{
		LicenseKey:  g.cfg.LicenseKey,
		MachineID:   g.fingerprint.MachineID(),
		ProjectSlug: g.cfg.ProjectSlug,
		OS:          osValue,
		Arch:        archValue,
	}
	if strings.TrimSpace(options.Version) != "" {
		body.Version = strings.TrimSpace(options.Version)
	}

	var resp PluginUpdatePackage
	path := "/api/v1/plugins/" + url.PathEscape(slug) + "/update"
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	raw, err := g.postJSON(ctx, path, bodyJSON)
	if err != nil {
		return nil, fmt.Errorf("request plugin update: %w", err)
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidServerResponse, err)
	}
	return &resp, nil
}

// UpdatePlugin performs a manual update for one plugin.
func (g *Guard) UpdatePlugin(ctx context.Context, slug string) error {
	if slug == "" {
		return fmt.Errorf("plugin slug is required")
	}

	catalog, err := g.GetPluginCatalog(ctx, true)
	if err != nil {
		return err
	}
	if catalog.UpdateFrozen {
		return ErrUpdateFrozen
	}

	var target *PluginInfo
	for i := range catalog.Plugins {
		if catalog.Plugins[i].Slug == slug {
			target = &catalog.Plugins[i]
			break
		}
	}
	if target == nil {
		return ErrPluginNotFound
	}
	if !target.OTAEnabled {
		return ErrPluginOTADisabled
	}
	if !target.UpdateAvailable {
		return ErrNoPluginUpdate
	}
	if !target.CanUpdate {
		return ErrNoPluginUpdate
	}
	if target.LatestVersion == nil || *target.LatestVersion == "" {
		return ErrNoPluginUpdate
	}

	u := updateInfo{
		Component:       slug,
		Latest:          *target.LatestVersion,
		UpdateAvailable: true,
	}

	if slug == g.cfg.ComponentSlug {
		oldVersion := g.currentVersion()
		if oldVersion == u.Latest {
			return nil
		}

		if err := g.updateBackend(u); err != nil {
			return err
		}
		return nil
	}

	mc, ok := g.findManagedComponent(slug)
	if !ok {
		return ErrPluginNotManaged
	}

	oldVersion := g.currentManagedVersion(slug)
	if oldVersion == u.Latest {
		return nil
	}

	switch mc.Strategy {
	case UpdateBackend:
		if err := g.updateManagedBackend(mc, u); err != nil {
			return err
		}
	default:
		if err := g.updateFrontend(mc, u); err != nil {
			return err
		}
	}

	return nil
}

func (g *Guard) resolveOTAPlatform(osOverride string, archOverride string) (string, string) {
	osValue := strings.TrimSpace(osOverride)
	archValue := strings.TrimSpace(archOverride)
	if osValue == "" {
		osValue = strings.TrimSpace(g.cfg.OTA.OS)
	}
	if archValue == "" {
		archValue = strings.TrimSpace(g.cfg.OTA.Arch)
	}
	if osValue == "" {
		osValue = "universal"
	}
	if archValue == "" {
		archValue = "universal"
	}
	return osValue, archValue
}

func (g *Guard) findManagedComponent(slug string) (ManagedComponent, bool) {
	for _, mc := range g.cfg.ManagedComponents {
		if mc.Slug == slug {
			return mc, true
		}
	}
	return ManagedComponent{}, false
}
