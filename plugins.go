package sdk

import (
	"context"
	"fmt"
	"net/url"
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
	if err := g.getJSON(ctx, "/api/v1/plugins/catalog", query, &resp); err != nil {
		return nil, fmt.Errorf("request plugin catalog: %w", err)
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

		g.updateBackend(u)
		if g.currentVersion() != u.Latest {
			return ErrUpdateApply
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
		g.updateManagedBackend(mc, u)
	default:
		g.updateFrontend(mc, u)
	}

	if g.currentManagedVersion(slug) != u.Latest {
		return ErrUpdateApply
	}

	return nil
}

func (g *Guard) findManagedComponent(slug string) (ManagedComponent, bool) {
	for _, mc := range g.cfg.ManagedComponents {
		if mc.Slug == slug {
			return mc, true
		}
	}
	return ManagedComponent{}, false
}
