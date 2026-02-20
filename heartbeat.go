package sdk

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

func (g *Guard) startHeartbeat(ctx context.Context) {
	interval := g.cfg.HeartbeatInterval
	graceStart := time.Time{}

	go func() {
		for {
			jitter := time.Duration(float64(interval) * (0.9 + rand.Float64()*0.2))
			select {
			case <-ctx.Done():
				return
			case <-time.After(jitter):
			}

			err := g.sendHeartbeat()
			if err == nil {
				g.sm.OnHeartbeatOK()
				graceStart = time.Time{}
				continue
			}

			if isFatalError(err) {
				g.sm.OnKill()
				return
			}

			// Network error → enter grace
			g.sm.OnHeartbeatFail()
			if graceStart.IsZero() {
				graceStart = time.Now()
			}

			if time.Since(graceStart) > g.cfg.GracePolicy.MaxOfflineDuration {
				g.sm.OnGracePeriodExpired()
				return
			}
		}
	}()
}

func (g *Guard) sendHeartbeat() error {
	// Snapshot version info under lock to avoid race
	g.mu.RLock()
	currentVersion := g.version
	managedVersionsSnapshot := make(map[string]string, len(g.managedVersions))
	for k, v := range g.managedVersions {
		managedVersionsSnapshot[k] = v
	}
	g.mu.RUnlock()

	components := []map[string]string{
		{
			"slug":    g.cfg.ComponentSlug,
			"version": currentVersion,
		},
	}

	for _, mc := range g.cfg.ManagedComponents {
		components = append(components, map[string]string{
			"slug":    mc.Slug,
			"version": managedVersionsSnapshot[mc.Slug],
		})
	}

	reqBody := map[string]any{
		"license_key":  g.cfg.LicenseKey,
		"machine_id":   g.fingerprint.MachineID(),
		"project_slug": g.cfg.ProjectSlug,
		"components":   components,
	}

	var resp heartbeatResponse
	if err := g.postJSON(context.Background(), "/api/v1/heartbeat", reqBody, &resp); err != nil {
		return fmt.Errorf("%w: %v", ErrNetworkError, err)
	}

	if resp.Status == "kill" {
		g.sm.OnKill()
		return ErrBanned
	}

	// Process update notifications
	if g.cfg.OTA.Enabled && !resp.UpdateFrozen {
		for _, u := range resp.Updates {
			if u.UpdateAvailable {
				g.handleUpdateNotification(u)
			}
		}
	}

	return nil
}

type heartbeatResponse struct {
	Status       string           `json:"status"`
	ServerTime   string           `json:"server_time"`
	NextInterval int              `json:"next_interval_s"`
	UpdateFrozen bool             `json:"update_frozen"`
	Updates      []updateInfo     `json:"updates"`
	Reason       string           `json:"reason"`
	Message      string           `json:"message"`
}

type updateInfo struct {
	Component       string `json:"component"`
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"update_available"`
	Mandatory       bool   `json:"mandatory"`
	ReleaseNotes    string `json:"release_notes"`
}

func isFatalError(err error) bool {
	return err == ErrBanned || err == ErrLicenseSuspended || err == ErrMachineBanned
}
