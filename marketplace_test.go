package sdk

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newMarketplaceTestGuard(t *testing.T, serverURL string) *Guard {
	t.Helper()

	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	guard, err := New(Config{
		ServerURL:     serverURL,
		LicenseKey:    "LIC-TEST-001",
		PublicKeyPEM:  pemEncodePublicKey(pubKey),
		ProjectSlug:   "demo-project",
		ComponentSlug: "backend",
		OTA: OTAConfig{
			OS:   "linux",
			Arch: "amd64",
		},
	})
	if err != nil {
		t.Fatalf("new guard: %v", err)
	}

	return guard
}

func TestMarketplaceCatalogDetailReviews(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/marketplace/browse":
			if got := r.URL.Query().Get("type"); got != "template" {
				t.Fatalf("unexpected type query: %s", got)
			}
			if got := r.URL.Query().Get("search"); got != "demo" {
				t.Fatalf("unexpected search query: %s", got)
			}
			if got := r.URL.Query().Get("target"); got != "backend" {
				t.Fatalf("unexpected target query: %s", got)
			}
			if got := r.URL.Query().Get("scope"); got != "extension" {
				t.Fatalf("unexpected scope query: %s", got)
			}
			if got := r.URL.Query().Get("os"); got != "linux" {
				t.Fatalf("unexpected os query: %s", got)
			}
			if got := r.URL.Query().Get("arch"); got != "amd64" {
				t.Fatalf("unexpected arch query: %s", got)
			}
			_ = json.NewEncoder(w).Encode(MarketplaceCatalog{
				Page:     1,
				PageSize: 20,
				Total:    1,
				Items: []MarketplaceItem{
					{
						Slug:             "demo",
						ItemType:         "template",
						Name:             "Demo Template",
						CurrentVersion:   "1.0.0",
						PackageSizeBytes: 1234,
						ThumbnailURL:     testString("/api/v1/marketplace/assets/marketplace%2Fassets%2Fitem-1%2Fthumbnail%2Fthumb.png"),
						ScreenshotURLs: []string{
							"/api/v1/marketplace/assets/marketplace%2Fassets%2Fitem-1%2Fscreenshots%2Fscreen.png",
						},
						Target:        testString("backend"),
						Scope:         testString("extension"),
						Manifest:      MarketplaceJSON{"entry": testRawJSON(`"plugin.js"`)},
						OS:            []string{"linux"},
						Arch:          []string{"amd64"},
						SDKVersionReq: testString(">=1.0.0"),
						Permissions:   []string{"net.http"},
						Dependencies:  map[string]string{"runtime": ">=1.0.0"},
						ConfigSchema:  MarketplaceJSON{"type": testRawJSON(`"object"`)},
						Status:        "published",
						CreatedAt:     "2026-01-01T00:00:00Z",
						UpdatedAt:     "2026-01-02T00:00:00Z",
						Stats: MarketplaceItemStats{
							RatingCount:  2,
							RatingAvg:    4.5,
							InstallCount: 8,
						},
					},
				},
			})
		case "/api/v1/marketplace/demo":
			if r.URL.Query().Get("license_key") == "" || r.URL.Query().Get("machine_id") == "" || r.URL.Query().Get("project_slug") == "" {
				t.Fatalf("expected license/machine/project query params")
			}
			_ = json.NewEncoder(w).Encode(MarketplaceDetail{
				Item: MarketplaceItem{
					Slug:             "demo",
					ItemType:         "template",
					Name:             "Demo Template",
					CurrentVersion:   "1.0.0",
					PackageSizeBytes: 1234,
					ThumbnailURL:     testString("/api/v1/marketplace/assets/marketplace%2Fassets%2Fitem-1%2Fthumbnail%2Fthumb.png"),
					ScreenshotURLs: []string{
						"/api/v1/marketplace/assets/marketplace%2Fassets%2Fitem-1%2Fscreenshots%2Fscreen.png",
					},
					Status:    "published",
					CreatedAt: "2026-01-01T00:00:00Z",
					UpdatedAt: "2026-01-02T00:00:00Z",
					Stats: MarketplaceItemStats{
						RatingCount:  2,
						RatingAvg:    4.5,
						InstallCount: 8,
					},
				},
				MyInstall: &MarketplaceInstallState{
					Status:           "installed",
					InstallCount:     1,
					InstalledVersion: testString("1.0.0"),
					LastInstallAt:    "2026-01-03T00:00:00Z",
				},
			})
		case "/api/v1/marketplace/demo/reviews":
			_ = json.NewEncoder(w).Encode(MarketplaceReviewList{
				Page:     1,
				PageSize: 20,
				Total:    1,
				Reviews: []MarketplaceReview{
					{
						ID:        "review-1",
						Score:     5,
						Title:     testString("Great"),
						Content:   testString("Nice template"),
						Customer:  testString("Acme"),
						CreatedAt: "2026-01-03T00:00:00Z",
						UpdatedAt: "2026-01-04T00:00:00Z",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	guard := newMarketplaceTestGuard(t, srv.URL)

	catalog, err := guard.GetMarketplaceCatalog(context.Background(), MarketplaceBrowseOptions{
		Type:   "template",
		Target: "backend",
		Scope:  "extension",
		OS:     "linux",
		Arch:   "amd64",
		Search: "demo",
	})
	if err != nil {
		t.Fatalf("get marketplace catalog: %v", err)
	}
	if catalog.Total != 1 || len(catalog.Items) != 1 {
		t.Fatalf("unexpected catalog payload: total=%d len=%d", catalog.Total, len(catalog.Items))
	}
	if catalog.Items[0].Slug != "demo" {
		t.Fatalf("unexpected item slug: %s", catalog.Items[0].Slug)
	}
	if catalog.Items[0].Target == nil || *catalog.Items[0].Target != "backend" {
		t.Fatalf("unexpected item target: %#v", catalog.Items[0].Target)
	}
	if catalog.Items[0].Scope == nil || *catalog.Items[0].Scope != "extension" {
		t.Fatalf("unexpected item scope: %#v", catalog.Items[0].Scope)
	}
	if len(catalog.Items[0].OS) != 1 || catalog.Items[0].OS[0] != "linux" {
		t.Fatalf("unexpected item os: %#v", catalog.Items[0].OS)
	}
	if string(catalog.Items[0].ConfigSchema["type"]) != `"object"` {
		t.Fatalf("unexpected config schema: %#v", catalog.Items[0].ConfigSchema)
	}
	if catalog.Items[0].ID != "" || catalog.Items[0].ComponentID != nil || catalog.Items[0].TemplateID != nil {
		t.Fatalf("catalog item should not expose internal IDs: %#v", catalog.Items[0])
	}
	if catalog.Items[0].ThumbnailURL == nil || *catalog.Items[0].ThumbnailURL == "" {
		t.Fatalf("expected public thumbnail URL, got %#v", catalog.Items[0].ThumbnailURL)
	}
	if len(catalog.Items[0].ScreenshotURLs) != 1 {
		t.Fatalf("expected public screenshot URL, got %#v", catalog.Items[0].ScreenshotURLs)
	}

	detail, err := guard.GetMarketplaceItem(context.Background(), "demo")
	if err != nil {
		t.Fatalf("get marketplace item: %v", err)
	}
	if detail.Item.Name != "Demo Template" {
		t.Fatalf("unexpected item name: %s", detail.Item.Name)
	}
	if detail.Item.ID != "" || detail.Item.ComponentID != nil || detail.Item.TemplateID != nil {
		t.Fatalf("detail item should not expose internal IDs: %#v", detail.Item)
	}
	if detail.Item.ThumbnailURL == nil || *detail.Item.ThumbnailURL == "" {
		t.Fatalf("expected public detail thumbnail URL, got %#v", detail.Item.ThumbnailURL)
	}
	if detail.MyInstall == nil || detail.MyInstall.Status != "installed" {
		t.Fatalf("unexpected my_install: %#v", detail.MyInstall)
	}

	reviews, err := guard.GetMarketplaceReviews(context.Background(), "demo", 1, 20)
	if err != nil {
		t.Fatalf("get marketplace reviews: %v", err)
	}
	if reviews.Total != 1 || len(reviews.Reviews) != 1 {
		t.Fatalf("unexpected reviews payload: total=%d len=%d", reviews.Total, len(reviews.Reviews))
	}
	if reviews.Reviews[0].Score != 5 {
		t.Fatalf("unexpected review score: %d", reviews.Reviews[0].Score)
	}
}

func TestMarketplaceInstallUninstallReview(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/marketplace/demo/install":
			var body marketplaceAccessBody
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.LicenseKey != "LIC-TEST-001" {
				t.Fatalf("unexpected license key: %v", body.LicenseKey)
			}
			if body.ProjectSlug != "demo-project" {
				t.Fatalf("unexpected project slug: %v", body.ProjectSlug)
			}
			if body.MachineID == "" {
				t.Fatalf("missing machine_id")
			}

			_ = json.NewEncoder(w).Encode(MarketplaceInstallPackage{
				Message:     "ready",
				Slug:        "demo",
				ItemType:    "template",
				Version:     "1.0.0",
				DownloadURL: "/api/v1/marketplace/assets/dl/token-1",
				SHA256:      "abc",
				Signature:   "sig",
				SizeBytes:   512,
				ExpiresIn:   300,
			})
		case "/api/v1/marketplace/demo/uninstall":
			var body marketplaceAccessBody
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.LicenseKey != "LIC-TEST-001" {
				t.Fatalf("unexpected license key in uninstall: %v", body.LicenseKey)
			}
			_ = json.NewEncoder(w).Encode(struct {
				Message string `json:"message"`
				Slug    string `json:"slug"`
			}{
				Message: "uninstalled",
				Slug:    "demo",
			})
		case "/api/v1/marketplace/demo/review":
			var body marketplaceReviewBody
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Score != 5 {
				t.Fatalf("unexpected score: %v", body.Score)
			}
			_ = json.NewEncoder(w).Encode(MarketplaceReviewSubmitResult{
				Message:  "review_submitted",
				ItemSlug: "demo",
				Stats: MarketplaceItemStats{
					RatingCount:  3,
					RatingAvg:    4.7,
					InstallCount: 10,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	guard := newMarketplaceTestGuard(t, srv.URL)

	pkg, err := guard.InstallMarketplaceItem(context.Background(), "demo")
	if err != nil {
		t.Fatalf("install marketplace item: %v", err)
	}
	if pkg.Message != "ready" || pkg.Slug != "demo" {
		t.Fatalf("unexpected install package response: %#v", pkg)
	}

	if err := guard.UninstallMarketplaceItem(context.Background(), "demo"); err != nil {
		t.Fatalf("uninstall marketplace item: %v", err)
	}

	reviewResp, err := guard.SubmitMarketplaceReview(context.Background(), "demo", 5, "Great", "Nice")
	if err != nil {
		t.Fatalf("submit marketplace review: %v", err)
	}
	if reviewResp.ItemSlug != "demo" || reviewResp.Stats.RatingCount != 3 {
		t.Fatalf("unexpected review submit response: %#v", reviewResp)
	}
}

func TestMarketplaceConfigureAndStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/marketplace/demo/configure":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected configure method: %s", r.Method)
			}
			var body marketplaceConfigureBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode configure body: %v", err)
			}
			if body.LicenseKey != "LIC-TEST-001" {
				t.Fatalf("unexpected configure license key: %v", body.LicenseKey)
			}
			if body.ProjectSlug != "demo-project" {
				t.Fatalf("unexpected configure project slug: %v", body.ProjectSlug)
			}
			if body.MachineID == "" {
				t.Fatalf("missing configure machine_id")
			}
			if string(body.Config["mode"]) != `"strict"` || string(body.Config["retries"]) != `3` {
				t.Fatalf("unexpected config payload: %#v", body.Config)
			}
			_ = json.NewEncoder(w).Encode(struct {
				Message string `json:"message"`
				Slug    string `json:"slug"`
			}{
				Message: "configured",
				Slug:    "demo",
			})
		case "/api/v1/marketplace/demo/status":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected status method: %s", r.Method)
			}
			var body marketplaceStatusBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode status body: %v", err)
			}
			if body.LicenseKey != "LIC-TEST-001" {
				t.Fatalf("unexpected status license key: %v", body.LicenseKey)
			}
			if body.ProjectSlug != "demo-project" {
				t.Fatalf("unexpected status project slug: %v", body.ProjectSlug)
			}
			if body.MachineID == "" {
				t.Fatalf("missing status machine_id")
			}
			if body.IsActive != false {
				t.Fatalf("unexpected active status: %v", body.IsActive)
			}
			if body.ErrorMessage != "startup failed" {
				t.Fatalf("unexpected error message: %v", body.ErrorMessage)
			}
			_ = json.NewEncoder(w).Encode(struct {
				Message  string `json:"message"`
				Slug     string `json:"slug"`
				IsActive bool   `json:"is_active"`
			}{
				Message:  "status_updated",
				Slug:     "demo",
				IsActive: false,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	guard := newMarketplaceTestGuard(t, srv.URL)
	if err := guard.ConfigureMarketplaceItem(context.Background(), "demo", MarketplaceConfig{
		"mode":    testRawJSON(`"strict"`),
		"retries": testRawJSON(`3`),
	}); err != nil {
		t.Fatalf("configure marketplace item: %v", err)
	}

	if err := guard.ReportMarketplaceStatus(context.Background(), "demo", false, " startup failed "); err != nil {
		t.Fatalf("report marketplace status: %v", err)
	}
}

func TestMarketplaceErrorMapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(testAPIErrorEnvelope{
			Error:   "install_required",
			Message: "install required",
		})
	}))
	defer srv.Close()

	guard := newMarketplaceTestGuard(t, srv.URL)
	_, err := guard.SubmitMarketplaceReview(context.Background(), "demo", 5, "Title", "Content")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var marketplaceErr *MarketplaceAPIError
	if !errors.As(err, &marketplaceErr) {
		t.Fatalf("expected MarketplaceAPIError, got %T (%v)", err, err)
	}
	if marketplaceErr.Code != "install_required" {
		t.Fatalf("unexpected error code: %s", marketplaceErr.Code)
	}
	if !IsMarketplaceError(err, "install_required") {
		t.Fatalf("expected IsMarketplaceError(err, install_required)=true")
	}
}
