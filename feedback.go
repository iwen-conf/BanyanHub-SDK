package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
)

// ---------------------------------------------------------------------------
// Feedback types
// ---------------------------------------------------------------------------

// FeedbackCategory represents the type of feedback.
type FeedbackCategory string

const (
	FeedbackBug        FeedbackCategory = "bug"
	FeedbackSuggestion FeedbackCategory = "suggestion"
	FeedbackQuestion   FeedbackCategory = "question"
)

// FeedbackStatus represents the processing state of a feedback item.
type FeedbackStatus string

const (
	FeedbackPending    FeedbackStatus = "pending"
	FeedbackProcessing FeedbackStatus = "processing"
	FeedbackResolved   FeedbackStatus = "resolved"
	FeedbackClosed     FeedbackStatus = "closed"
)

// SubmitFeedbackRequest is the payload for submitting new feedback.
type SubmitFeedbackRequest struct {
	UserID      string               `json:"user_id"`
	UserName    string               `json:"user_name"`
	UserEmail   string               `json:"user_email,omitempty"`
	Category    FeedbackCategory     `json:"category"`
	Title       string               `json:"title"`
	Content     string               `json:"content"`
	AppVersion  string               `json:"app_version,omitempty"`
	Attachments []FeedbackAttachment `json:"attachments,omitempty"`
}

type submitFeedbackBody struct {
	LicenseKey  string               `json:"license_key"`
	MachineID   string               `json:"machine_id"`
	ProjectSlug string               `json:"project_slug"`
	UserID      string               `json:"user_id"`
	UserName    string               `json:"user_name"`
	UserEmail   string               `json:"user_email,omitempty"`
	Category    FeedbackCategory     `json:"category"`
	Title       string               `json:"title"`
	Content     string               `json:"content"`
	AppVersion  string               `json:"app_version,omitempty"`
	Attachments []FeedbackAttachment `json:"attachments,omitempty"`
}

// FeedbackAttachment describes a file attached to a feedback submission.
type FeedbackAttachment struct {
	Kind        string `json:"kind"`
	FileKey     string `json:"file_key"`
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
}

// FeedbackItem represents a single feedback entry returned by the server.
type FeedbackItem struct {
	ID          string                   `json:"id"`
	Category    FeedbackCategory         `json:"category"`
	Status      FeedbackStatus           `json:"status"`
	Title       string                   `json:"title"`
	Content     string                   `json:"content"`
	AppVersion  string                   `json:"app_version,omitempty"`
	Attachments []FeedbackAttachmentInfo `json:"attachments,omitempty"`
	Replies     []FeedbackReply          `json:"replies,omitempty"`
	CreatedAt   string                   `json:"created_at"`
	UpdatedAt   string                   `json:"updated_at"`
}

// FeedbackAttachmentInfo is the server-side representation of an attachment.
type FeedbackAttachmentInfo struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	FileKey     string `json:"file_key"`
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type,omitempty"`
	SizeBytes   int64  `json:"size_bytes"`
}

// FeedbackReply is a single reply on a feedback item.
type FeedbackReply struct {
	ID         string `json:"id"`
	Author     string `json:"author"`
	AuthorRole string `json:"author_role"`
	Content    string `json:"content"`
	CreatedAt  string `json:"created_at"`
}

// FeedbackListResponse wraps a paginated list of feedback items.
type FeedbackListResponse struct {
	Feedbacks  []FeedbackItem         `json:"data"`
	Pagination FeedbackListPagination `json:"pagination"`
}

// FeedbackListPagination holds the pagination info returned by the server.
type FeedbackListPagination struct {
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

// convenience accessors for backward compatibility
func (r *FeedbackListResponse) Total() int   { return r.Pagination.Total }
func (r *FeedbackListResponse) PageNum() int { return r.Pagination.Page }
func (r *FeedbackListResponse) Size() int    { return r.Pagination.PageSize }

// UploadURLResponse is returned after a successful attachment upload.
type UploadURLResponse struct {
	UploadURL string `json:"upload_url"`
	FileKey   string `json:"file_key"`
}

type prepareFeedbackUploadBody struct {
	LicenseKey  string `json:"license_key"`
	ProjectSlug string `json:"project_slug"`
	FileName    string `json:"file_name"`
}

// ReleaseNoteEntry represents a single version's release notes.
type ReleaseNoteEntry struct {
	ComponentSlug     string             `json:"component_slug,omitempty"`
	Version           string             `json:"version"`
	ReleaseNotes      string             `json:"release_notes"`
	ResolvedFeedbacks []ResolvedFeedback `json:"resolved_feedbacks,omitempty"`
	CreatedAt         string             `json:"created_at"`
}

// ResolvedFeedback is a feedback item resolved in a release.
type ResolvedFeedback struct {
	ID       string           `json:"id"`
	Title    string           `json:"title"`
	Category FeedbackCategory `json:"category"`
}

// ReleaseNotesResponse wraps the list of release note entries.
type ReleaseNotesResponse struct {
	Entries []ReleaseNoteEntry `json:"entries"`
}

// ---------------------------------------------------------------------------
// Guard methods
// ---------------------------------------------------------------------------

// SubmitFeedback submits a new feedback item to BanyanHub.
func (g *Guard) SubmitFeedback(ctx context.Context, req SubmitFeedbackRequest) (*FeedbackItem, error) {
	body := submitFeedbackBody{
		LicenseKey:  g.cfg.LicenseKey,
		MachineID:   g.fingerprint.MachineID(),
		ProjectSlug: g.cfg.ProjectSlug,
		UserID:      req.UserID,
		UserName:    req.UserName,
		UserEmail:   req.UserEmail,
		Category:    req.Category,
		Title:       req.Title,
		Content:     req.Content,
		AppVersion:  req.AppVersion,
		Attachments: req.Attachments,
	}

	var item FeedbackItem
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	raw, err := g.postJSON(ctx, "/api/v1/feedbacks", bodyJSON)
	if err != nil {
		return nil, fmt.Errorf("submit feedback: %w", err)
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidServerResponse, err)
	}
	return &item, nil
}

// ListMyFeedback returns a paginated list of feedback items for the given user.
func (g *Guard) ListMyFeedback(ctx context.Context, userID string, page, pageSize int) (*FeedbackListResponse, error) {
	query := url.Values{}
	query.Set("license_key", g.cfg.LicenseKey)
	query.Set("project_slug", g.cfg.ProjectSlug)
	query.Set("user_id", userID)
	query.Set("page", strconv.Itoa(page))
	query.Set("page_size", strconv.Itoa(pageSize))

	var resp FeedbackListResponse
	raw, err := g.getJSON(ctx, "/api/v1/feedbacks", query)
	if err != nil {
		return nil, fmt.Errorf("list feedback: %w", err)
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidServerResponse, err)
	}
	return &resp, nil
}

// UploadFeedbackFile uploads an attachment for use in a feedback submission.
// The returned UploadURLResponse contains the file_key to reference in
// SubmitFeedbackRequest.Attachments.
func (g *Guard) UploadFeedbackFile(ctx context.Context, fileName string, contentType string, data io.Reader) (*UploadURLResponse, error) {
	uploadTarget, err := g.prepareFeedbackUpload(ctx, fileName)
	if err != nil {
		return nil, err
	}
	if uploadTarget.FileKey == "" {
		return nil, ErrInvalidServerResponse
	}

	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		var err error
		defer func() { pw.CloseWithError(err) }()

		_ = writer.WriteField("license_key", g.cfg.LicenseKey)
		_ = writer.WriteField("project_slug", g.cfg.ProjectSlug)
		_ = writer.WriteField("file_key", uploadTarget.FileKey)
		if contentType != "" {
			_ = writer.WriteField("content_type", contentType)
		}

		var part io.Writer
		part, err = writer.CreateFormFile("file", fileName)
		if err != nil {
			return
		}
		_, err = io.Copy(part, data)
		if err != nil {
			return
		}
		err = writer.Close()
	}()

	fullURL := g.feedbackUploadURL(uploadTarget.UploadURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, pr)
	if err != nil {
		return nil, fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "BanyanHub-SDK/"+Version)

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetworkError, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, decodeAPIErrorResponse(resp)
	}

	var result UploadURLResponse
	raw, err := readAPIJSONResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidServerResponse, err)
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidServerResponse, err)
	}
	if result.FileKey == "" {
		result.FileKey = uploadTarget.FileKey
	}
	if result.UploadURL == "" {
		result.UploadURL = uploadTarget.UploadURL
	}
	return &result, nil
}

func (g *Guard) prepareFeedbackUpload(ctx context.Context, fileName string) (*UploadURLResponse, error) {
	body := prepareFeedbackUploadBody{
		LicenseKey:  g.cfg.LicenseKey,
		ProjectSlug: g.cfg.ProjectSlug,
		FileName:    fileName,
	}

	var resp UploadURLResponse
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	raw, err := g.postJSON(ctx, "/api/v1/feedbacks/upload-url", bodyJSON)
	if err != nil {
		return nil, fmt.Errorf("prepare feedback upload: %w", err)
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidServerResponse, err)
	}
	if resp.UploadURL == "" {
		resp.UploadURL = "/api/v1/feedbacks/upload"
	}
	return &resp, nil
}

func (g *Guard) feedbackUploadURL(uploadURL string) string {
	if uploadURL == "" {
		uploadURL = "/api/v1/feedbacks/upload"
	}
	return serverURLForPath(g.cfg.ServerURL, uploadURL)
}

// FetchReleaseNotes retrieves the release notes grouped by version.
func (g *Guard) FetchReleaseNotes(ctx context.Context) (*ReleaseNotesResponse, error) {
	query := url.Values{}
	query.Set("license_key", g.cfg.LicenseKey)
	query.Set("project_slug", g.cfg.ProjectSlug)

	var wire releaseNotesWireResponse
	raw, err := g.getJSON(ctx, "/api/v1/feedbacks/release-notes", query)
	if err != nil {
		return nil, fmt.Errorf("fetch release notes: %w", err)
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidServerResponse, err)
	}
	return wire.toSDKResponse(), nil
}

type releaseNotesWireResponse struct {
	Entries  []ReleaseNoteEntry     `json:"entries"`
	Releases []releaseNotesWireItem `json:"releases"`
}

type releaseNotesWireItem struct {
	ComponentSlug     string             `json:"component_slug"`
	Version           string             `json:"version"`
	ReleaseNotes      *string            `json:"release_notes"`
	ResolvedFeedbacks []ResolvedFeedback `json:"resolved_feedbacks"`
	Feedbacks         []ResolvedFeedback `json:"feedbacks"`
	CreatedAt         string             `json:"created_at"`
}

func (r releaseNotesWireResponse) toSDKResponse() *ReleaseNotesResponse {
	if len(r.Entries) > 0 || len(r.Releases) == 0 {
		return &ReleaseNotesResponse{Entries: r.Entries}
	}

	entries := make([]ReleaseNoteEntry, 0, len(r.Releases))
	for _, release := range r.Releases {
		feedbacks := release.ResolvedFeedbacks
		if len(feedbacks) == 0 {
			feedbacks = release.Feedbacks
		}

		notes := ""
		if release.ReleaseNotes != nil {
			notes = *release.ReleaseNotes
		}

		entries = append(entries, ReleaseNoteEntry{
			ComponentSlug:     release.ComponentSlug,
			Version:           release.Version,
			ReleaseNotes:      notes,
			ResolvedFeedbacks: feedbacks,
			CreatedAt:         release.CreatedAt,
		})
	}
	return &ReleaseNotesResponse{Entries: entries}
}
