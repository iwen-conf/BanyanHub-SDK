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
			_ = json.NewEncoder(w).Encode(map[string]any{
				"page":      1,
				"page_size": 20,
				"total":     1,
				"items": []map[string]any{
					{
						"id":                 "item-1",
						"slug":               "demo",
						"item_type":          "template",
						"name":               "Demo Template",
						"current_version":    "1.0.0",
						"package_size_bytes": 1234,
						"screenshots":        []string{},
						"status":             "published",
						"created_at":         "2026-01-01T00:00:00Z",
						"updated_at":         "2026-01-02T00:00:00Z",
						"stats": map[string]any{
							"rating_count":  2,
							"rating_avg":    4.5,
							"install_count": 8,
						},
					},
				},
			})
		case "/api/v1/marketplace/demo":
			if r.URL.Query().Get("license_key") == "" || r.URL.Query().Get("machine_id") == "" || r.URL.Query().Get("project_slug") == "" {
				t.Fatalf("expected license/machine/project query params")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"item": map[string]any{
					"id":                 "item-1",
					"slug":               "demo",
					"item_type":          "template",
					"name":               "Demo Template",
					"current_version":    "1.0.0",
					"package_size_bytes": 1234,
					"screenshots":        []string{},
					"status":             "published",
					"created_at":         "2026-01-01T00:00:00Z",
					"updated_at":         "2026-01-02T00:00:00Z",
					"stats": map[string]any{
						"rating_count":  2,
						"rating_avg":    4.5,
						"install_count": 8,
					},
				},
				"my_install": map[string]any{
					"status":            "installed",
					"install_count":     1,
					"installed_version": "1.0.0",
					"last_install_at":   "2026-01-03T00:00:00Z",
				},
			})
		case "/api/v1/marketplace/demo/reviews":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"page":      1,
				"page_size": 20,
				"total":     1,
				"reviews": []map[string]any{
					{
						"id":         "review-1",
						"score":      5,
						"title":      "Great",
						"content":    "Nice template",
						"customer":   "Acme",
						"created_at": "2026-01-03T00:00:00Z",
						"updated_at": "2026-01-04T00:00:00Z",
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

	detail, err := guard.GetMarketplaceItem(context.Background(), "demo")
	if err != nil {
		t.Fatalf("get marketplace item: %v", err)
	}
	if detail.Item.Name != "Demo Template" {
		t.Fatalf("unexpected item name: %s", detail.Item.Name)
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
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["license_key"] != "LIC-TEST-001" {
				t.Fatalf("unexpected license key: %v", body["license_key"])
			}
			if body["project_slug"] != "demo-project" {
				t.Fatalf("unexpected project slug: %v", body["project_slug"])
			}
			if body["machine_id"] == "" {
				t.Fatalf("missing machine_id")
			}

			_ = json.NewEncoder(w).Encode(map[string]any{
				"message":      "ready",
				"slug":         "demo",
				"item_type":    "template",
				"version":      "1.0.0",
				"download_url": "/api/v1/marketplace/assets/dl/token-1",
				"sha256":       "abc",
				"signature":    "sig",
				"size_bytes":   512,
				"expires_in":   300,
			})
		case "/api/v1/marketplace/demo/uninstall":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["license_key"] != "LIC-TEST-001" {
				t.Fatalf("unexpected license key in uninstall: %v", body["license_key"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"message": "uninstalled",
				"slug":    "demo",
			})
		case "/api/v1/marketplace/demo/review":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["score"] != float64(5) {
				t.Fatalf("unexpected score: %v", body["score"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"message":   "review_submitted",
				"item_slug": "demo",
				"stats": map[string]any{
					"rating_count":  3,
					"rating_avg":    4.7,
					"install_count": 10,
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

func TestMarketplaceErrorMapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":   "install_required",
			"message": "install required",
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
