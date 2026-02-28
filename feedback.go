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
	ID          string                 `json:"id"`
	Category    FeedbackCategory       `json:"category"`
	Status      FeedbackStatus         `json:"status"`
	Title       string                 `json:"title"`
	Content     string                 `json:"content"`
	AppVersion  string                 `json:"app_version,omitempty"`
	Attachments []FeedbackAttachmentInfo `json:"attachments,omitempty"`
	Replies     []FeedbackReply        `json:"replies,omitempty"`
	CreatedAt   string                 `json:"created_at"`
	UpdatedAt   string                 `json:"updated_at"`
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
	Feedbacks []FeedbackItem `json:"feedbacks"`
	Total     int            `json:"total"`
	Page      int            `json:"page"`
	PageSize  int            `json:"page_size"`
}

// UploadURLResponse is returned after a successful attachment upload.
type UploadURLResponse struct {
	UploadURL string `json:"upload_url"`
	FileKey   string `json:"file_key"`
}

// ReleaseNoteEntry represents a single version's release notes.
type ReleaseNoteEntry struct {
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
	body := map[string]any{
		"license_key":   g.cfg.LicenseKey,
		"machine_id":    g.fingerprint.MachineID(),
		"project_slug":  g.cfg.ProjectSlug,
		"user_id":       req.UserID,
		"user_name":     req.UserName,
		"category":      req.Category,
		"title":         req.Title,
		"content":       req.Content,
	}
	if req.UserEmail != "" {
		body["user_email"] = req.UserEmail
	}
	if req.AppVersion != "" {
		body["app_version"] = req.AppVersion
	}
	if len(req.Attachments) > 0 {
		body["attachments"] = req.Attachments
	}

	var item FeedbackItem
	if err := g.postJSON(ctx, "/api/v1/feedbacks", body, &item); err != nil {
		return nil, fmt.Errorf("submit feedback: %w", err)
	}
	return &item, nil
}

// ListMyFeedback returns a paginated list of feedback items for the given user.
func (g *Guard) ListMyFeedback(ctx context.Context, userID string, page, pageSize int) (*FeedbackListResponse, error) {
	query := url.Values{}
	query.Set("license_key", g.cfg.LicenseKey)
	query.Set("user_id", userID)
	query.Set("page", strconv.Itoa(page))
	query.Set("page_size", strconv.Itoa(pageSize))

	var resp FeedbackListResponse
	if err := g.getJSON(ctx, "/api/v1/feedbacks", query, &resp); err != nil {
		return nil, fmt.Errorf("list feedback: %w", err)
	}
	return &resp, nil
}

// UploadFeedbackFile uploads an attachment for use in a feedback submission.
// The returned UploadURLResponse contains the file_key to reference in
// SubmitFeedbackRequest.Attachments.
func (g *Guard) UploadFeedbackFile(ctx context.Context, fileName string, contentType string, data io.Reader) (*UploadURLResponse, error) {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		var err error
		defer func() { pw.CloseWithError(err) }()

		_ = writer.WriteField("license_key", g.cfg.LicenseKey)
		_ = writer.WriteField("project_slug", g.cfg.ProjectSlug)

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

	fullURL := g.cfg.ServerURL + "/api/v1/feedbacks/upload"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, pr)
	if err != nil {
		return nil, fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send upload request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrInvalidServerResponse, resp.StatusCode)
	}

	var result UploadURLResponse
	if err := decodeJSON(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidServerResponse, err)
	}
	return &result, nil
}

// FetchReleaseNotes retrieves the release notes grouped by version.
func (g *Guard) FetchReleaseNotes(ctx context.Context) (*ReleaseNotesResponse, error) {
	query := url.Values{}
	query.Set("project_slug", g.cfg.ProjectSlug)

	var resp ReleaseNotesResponse
	if err := g.getJSON(ctx, "/api/v1/feedbacks/release-notes", query, &resp); err != nil {
		return nil, fmt.Errorf("fetch release notes: %w", err)
	}
	return &resp, nil
}

// decodeJSON is a small helper to decode JSON from a reader.
func decodeJSON(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}
