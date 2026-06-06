package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type MarketplaceItemStats struct {
	RatingCount  int     `json:"rating_count"`
	RatingAvg    float64 `json:"rating_avg"`
	InstallCount int     `json:"install_count"`
}

type MarketplaceJSON map[string]json.RawMessage
type MarketplaceConfig MarketplaceJSON

type MarketplaceItem struct {
	ID               string               `json:"id"`
	Slug             string               `json:"slug"`
	ItemType         string               `json:"item_type"`
	Name             string               `json:"name"`
	Summary          *string              `json:"summary"`
	Description      *string              `json:"description"`
	Category         *string              `json:"category"`
	Tags             []string             `json:"tags"`
	ComponentID      *string              `json:"component_id"`
	TemplateID       *string              `json:"template_id"`
	CurrentVersion   string               `json:"current_version"`
	PackageSizeBytes int64                `json:"package_size_bytes"`
	ThumbnailKey     *string              `json:"thumbnail_key"`
	ThumbnailURL     *string              `json:"thumbnail_url"`
	Screenshots      []string             `json:"screenshots"`
	ScreenshotURLs   []string             `json:"screenshot_urls"`
	Target           *string              `json:"target"`
	Scope            *string              `json:"scope"`
	Manifest         MarketplaceJSON      `json:"manifest"`
	OS               []string             `json:"os"`
	Arch             []string             `json:"arch"`
	SDKVersionReq    *string              `json:"sdk_version_req"`
	Permissions      []string             `json:"permissions"`
	Dependencies     map[string]string    `json:"dependencies"`
	ConfigSchema     MarketplaceJSON      `json:"config_schema"`
	Status           string               `json:"status"`
	CreatedAt        string               `json:"created_at"`
	UpdatedAt        string               `json:"updated_at"`
	Stats            MarketplaceItemStats `json:"stats"`
}

type MarketplaceInstallState struct {
	Status           string  `json:"status"`
	InstallCount     int     `json:"install_count"`
	InstalledVersion *string `json:"installed_version"`
	LastInstallAt    string  `json:"last_install_at"`
	LastUninstallAt  *string `json:"last_uninstall_at"`
}

type MarketplaceDetail struct {
	Item      MarketplaceItem          `json:"item"`
	MyInstall *MarketplaceInstallState `json:"my_install"`
}

type MarketplaceCatalog struct {
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
	Total    int               `json:"total"`
	Items    []MarketplaceItem `json:"items"`
}

type MarketplaceReview struct {
	ID        string  `json:"id"`
	Score     int     `json:"score"`
	Title     *string `json:"title"`
	Content   *string `json:"content"`
	Customer  *string `json:"customer"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

type MarketplaceReviewList struct {
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
	Total    int                 `json:"total"`
	Reviews  []MarketplaceReview `json:"reviews"`
}

type MarketplaceInstallPackage struct {
	Message     string `json:"message"`
	Slug        string `json:"slug"`
	ItemType    string `json:"item_type"`
	Version     string `json:"version"`
	DownloadURL string `json:"download_url"`
	SHA256      string `json:"sha256"`
	Signature   string `json:"signature"`
	SizeBytes   int64  `json:"size_bytes"`
	ExpiresIn   int    `json:"expires_in"`
}

type MarketplaceReviewSubmitResult struct {
	Message  string               `json:"message"`
	ItemSlug string               `json:"item_slug"`
	Stats    MarketplaceItemStats `json:"stats"`
}

type MarketplaceBrowseOptions struct {
	Type     string
	Category string
	Target   string
	Scope    string
	OS       string
	Arch     string
	Search   string
	Sort     string
	Page     int
	PageSize int
}

type marketplaceAccessBody struct {
	LicenseKey  string `json:"license_key"`
	MachineID   string `json:"machine_id"`
	ProjectSlug string `json:"project_slug"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
}

type marketplaceConfigureBody struct {
	marketplaceAccessBody
	Config MarketplaceConfig `json:"config"`
}

type marketplaceStatusBody struct {
	marketplaceAccessBody
	IsActive     bool   `json:"is_active"`
	ErrorMessage string `json:"error_message,omitempty"`
}

type marketplaceReviewBody struct {
	marketplaceAccessBody
	Score   int    `json:"score"`
	Title   string `json:"title,omitempty"`
	Content string `json:"content,omitempty"`
}

type MarketplaceAPIError = APIError

// IsMarketplaceError reports whether err contains MarketplaceAPIError.
// If code is empty, this only checks the error type.
func IsMarketplaceError(err error, code string) bool {
	var marketplaceErr *MarketplaceAPIError
	if !errors.As(err, &marketplaceErr) {
		return false
	}
	if code == "" {
		return true
	}
	return marketplaceErr.Code == code
}

func (g *Guard) marketplaceAccessBody() marketplaceAccessBody {
	return marketplaceAccessBody{
		LicenseKey:  g.cfg.LicenseKey,
		MachineID:   g.fingerprint.MachineID(),
		ProjectSlug: g.cfg.ProjectSlug,
		OS:          g.cfg.OTA.OS,
		Arch:        g.cfg.OTA.Arch,
	}
}

func (g *Guard) marketplaceRequest(ctx context.Context, method, path string, query url.Values, data []byte) ([]byte, error) {
	fullURL := serverURLForPath(g.cfg.ServerURL, path)
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	var payload io.Reader
	if data != nil {
		payload = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, payload)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "BanyanHub-SDK/"+Version)
	if data != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetworkError, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, decodeAPIErrorResponse(resp)
	}
	raw, err := readAPIJSONResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidServerResponse, err)
	}
	return raw, nil
}

func (g *Guard) GetMarketplaceCatalog(ctx context.Context, options MarketplaceBrowseOptions) (*MarketplaceCatalog, error) {
	query := url.Values{}
	if options.Type != "" {
		query.Set("type", options.Type)
	}
	if options.Category != "" {
		query.Set("category", options.Category)
	}
	if options.Target != "" {
		query.Set("target", options.Target)
	}
	if options.Scope != "" {
		query.Set("scope", options.Scope)
	}
	if options.OS != "" {
		query.Set("os", options.OS)
	}
	if options.Arch != "" {
		query.Set("arch", options.Arch)
	}
	if options.Search != "" {
		query.Set("search", options.Search)
	}

	sortBy := options.Sort
	if sortBy == "" {
		sortBy = "latest"
	}
	query.Set("sort", sortBy)

	page := options.Page
	if page <= 0 {
		page = 1
	}
	pageSize := options.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	query.Set("page", strconv.Itoa(page))
	query.Set("page_size", strconv.Itoa(pageSize))

	var resp MarketplaceCatalog
	raw, err := g.marketplaceRequest(ctx, http.MethodGet, "/api/v1/marketplace/browse", query, nil)
	if err != nil {
		return nil, fmt.Errorf("request marketplace catalog: %w", err)
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidServerResponse, err)
	}
	return &resp, nil
}

func (g *Guard) GetMarketplaceItem(ctx context.Context, slug string) (*MarketplaceDetail, error) {
	if slug == "" {
		return nil, fmt.Errorf("marketplace slug is required")
	}

	query := url.Values{}
	query.Set("license_key", g.cfg.LicenseKey)
	query.Set("machine_id", g.fingerprint.MachineID())
	query.Set("project_slug", g.cfg.ProjectSlug)

	var resp MarketplaceDetail
	path := "/api/v1/marketplace/" + url.PathEscape(slug)
	raw, err := g.marketplaceRequest(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return nil, fmt.Errorf("request marketplace item: %w", err)
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidServerResponse, err)
	}
	return &resp, nil
}

func (g *Guard) GetMarketplaceReviews(ctx context.Context, slug string, page, pageSize int) (*MarketplaceReviewList, error) {
	if slug == "" {
		return nil, fmt.Errorf("marketplace slug is required")
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	query := url.Values{}
	query.Set("page", strconv.Itoa(page))
	query.Set("page_size", strconv.Itoa(pageSize))

	var resp MarketplaceReviewList
	path := "/api/v1/marketplace/" + url.PathEscape(slug) + "/reviews"
	raw, err := g.marketplaceRequest(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return nil, fmt.Errorf("request marketplace reviews: %w", err)
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidServerResponse, err)
	}
	return &resp, nil
}

func (g *Guard) InstallMarketplaceItem(ctx context.Context, slug string) (*MarketplaceInstallPackage, error) {
	if slug == "" {
		return nil, fmt.Errorf("marketplace slug is required")
	}

	var resp MarketplaceInstallPackage
	path := "/api/v1/marketplace/" + url.PathEscape(slug) + "/install"
	bodyJSON, err := json.Marshal(g.marketplaceAccessBody())
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	raw, err := g.marketplaceRequest(ctx, http.MethodPost, path, nil, bodyJSON)
	if err != nil {
		return nil, fmt.Errorf("install marketplace item: %w", err)
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidServerResponse, err)
	}
	return &resp, nil
}

func (g *Guard) UninstallMarketplaceItem(ctx context.Context, slug string) error {
	if slug == "" {
		return fmt.Errorf("marketplace slug is required")
	}

	path := "/api/v1/marketplace/" + url.PathEscape(slug) + "/uninstall"
	bodyJSON, err := json.Marshal(g.marketplaceAccessBody())
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	if _, err := g.marketplaceRequest(ctx, http.MethodPost, path, nil, bodyJSON); err != nil {
		return fmt.Errorf("uninstall marketplace item: %w", err)
	}
	return nil
}

func (g *Guard) ConfigureMarketplaceItem(ctx context.Context, slug string, config MarketplaceConfig) error {
	if slug == "" {
		return fmt.Errorf("marketplace slug is required")
	}
	if config == nil {
		config = MarketplaceConfig{}
	}

	body := marketplaceConfigureBody{
		marketplaceAccessBody: g.marketplaceAccessBody(),
		Config:                config,
	}

	path := "/api/v1/marketplace/" + url.PathEscape(slug) + "/configure"
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	if _, err := g.marketplaceRequest(ctx, http.MethodPost, path, nil, bodyJSON); err != nil {
		return fmt.Errorf("configure marketplace item: %w", err)
	}
	return nil
}

func (g *Guard) ReportMarketplaceStatus(ctx context.Context, slug string, isActive bool, errorMessage string) error {
	if slug == "" {
		return fmt.Errorf("marketplace slug is required")
	}

	body := marketplaceStatusBody{
		marketplaceAccessBody: g.marketplaceAccessBody(),
		IsActive:              isActive,
	}
	if strings.TrimSpace(errorMessage) != "" {
		body.ErrorMessage = strings.TrimSpace(errorMessage)
	}

	path := "/api/v1/marketplace/" + url.PathEscape(slug) + "/status"
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	if _, err := g.marketplaceRequest(ctx, http.MethodPost, path, nil, bodyJSON); err != nil {
		return fmt.Errorf("report marketplace status: %w", err)
	}
	return nil
}

func (g *Guard) SubmitMarketplaceReview(
	ctx context.Context,
	slug string,
	score int,
	title string,
	content string,
) (*MarketplaceReviewSubmitResult, error) {
	if slug == "" {
		return nil, fmt.Errorf("marketplace slug is required")
	}
	if score < 1 || score > 5 {
		return nil, fmt.Errorf("score must be in range [1,5]")
	}

	body := marketplaceReviewBody{
		marketplaceAccessBody: g.marketplaceAccessBody(),
		Score:                 score,
	}
	if strings.TrimSpace(title) != "" {
		body.Title = strings.TrimSpace(title)
	}
	if strings.TrimSpace(content) != "" {
		body.Content = strings.TrimSpace(content)
	}

	var resp MarketplaceReviewSubmitResult
	path := "/api/v1/marketplace/" + url.PathEscape(slug) + "/review"
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	raw, err := g.marketplaceRequest(ctx, http.MethodPost, path, nil, bodyJSON)
	if err != nil {
		return nil, fmt.Errorf("submit marketplace review: %w", err)
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidServerResponse, err)
	}
	return &resp, nil
}
