package sdk

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newFeedbackTestGuard(t *testing.T, serverURL string) *Guard {
	t.Helper()

	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	guard, err := New(Config{
		ServerURL:     serverURL,
		LicenseKey:    "LIC-FEEDBACK-001",
		PublicKeyPEM:  pemEncodePublicKey(pubKey),
		ProjectSlug:   "demo-project",
		ComponentSlug: "backend",
	})
	if err != nil {
		t.Fatalf("new guard: %v", err)
	}
	return guard
}

func TestUploadFeedbackFile_UsesPreparedFileKey(t *testing.T) {
	const fileKey = "feedbacks/demo-project/upload-1/screenshot.png"
	const payload = "fake-image-bytes"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/feedbacks/upload-url":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode prepare body: %v", err)
			}
			if body["license_key"] != "LIC-FEEDBACK-001" {
				t.Fatalf("unexpected license key: %s", body["license_key"])
			}
			if body["project_slug"] != "demo-project" {
				t.Fatalf("unexpected project slug: %s", body["project_slug"])
			}
			if body["file_name"] != "screenshot.png" {
				t.Fatalf("unexpected file name: %s", body["file_name"])
			}

			_ = json.NewEncoder(w).Encode(map[string]string{
				"upload_url": "/api/v1/feedbacks/upload",
				"file_key":   fileKey,
			})
		case "/api/v1/feedbacks/upload":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("parse multipart: %v", err)
			}
			if got := r.FormValue("license_key"); got != "LIC-FEEDBACK-001" {
				t.Fatalf("unexpected upload license key: %s", got)
			}
			if got := r.FormValue("project_slug"); got != "demo-project" {
				t.Fatalf("unexpected upload project slug: %s", got)
			}
			if got := r.FormValue("file_key"); got != fileKey {
				t.Fatalf("unexpected file key: %s", got)
			}

			file, _, err := r.FormFile("file")
			if err != nil {
				t.Fatalf("read form file: %v", err)
			}
			defer file.Close()
			got, err := io.ReadAll(file)
			if err != nil {
				t.Fatalf("read upload body: %v", err)
			}
			if string(got) != payload {
				t.Fatalf("unexpected upload payload: %s", string(got))
			}

			_ = json.NewEncoder(w).Encode(map[string]any{
				"file_key":   fileKey,
				"size_bytes": len(payload),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	guard := newFeedbackTestGuard(t, srv.URL)
	result, err := guard.UploadFeedbackFile(context.Background(), "screenshot.png", "image/png", bytes.NewBufferString(payload))
	if err != nil {
		t.Fatalf("upload feedback file: %v", err)
	}
	if result.FileKey != fileKey {
		t.Fatalf("unexpected result file key: %s", result.FileKey)
	}
}

func TestUploadFeedbackFile_DecodesStructuredAPIError(t *testing.T) {
	const fileKey = "feedbacks/demo-project/upload-1/screenshot.png"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/feedbacks/upload-url":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"upload_url": "/api/v1/feedbacks/upload",
				"file_key":   fileKey,
			})
		case "/api/v1/feedbacks/upload":
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":   "invalid_file_key",
				"message": "file key does not match project scope",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	guard := newFeedbackTestGuard(t, srv.URL)
	_, err := guard.UploadFeedbackFile(context.Background(), "screenshot.png", "image/png", bytes.NewBufferString("payload"))
	if err == nil {
		t.Fatal("expected upload error")
	}
	if !errors.Is(err, ErrUploadInvalid) {
		t.Fatalf("expected ErrUploadInvalid, got %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Code != "invalid_file_key" || apiErr.Message != "file key does not match project scope" {
		t.Fatalf("unexpected API error payload: %#v", apiErr)
	}
}

func TestUploadFeedbackFile_PrepareUploadUsesLicenseInvalid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/feedbacks/upload-url" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":   "license_invalid",
			"message": "license is not authorized for this feedback project",
		})
	}))
	defer srv.Close()

	guard := newFeedbackTestGuard(t, srv.URL)
	_, err := guard.UploadFeedbackFile(context.Background(), "screenshot.png", "image/png", bytes.NewBufferString("payload"))
	if err == nil {
		t.Fatal("expected upload preparation error")
	}
	if !errors.Is(err, ErrLicenseInvalid) {
		t.Fatalf("expected ErrLicenseInvalid, got %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Code != "license_invalid" {
		t.Fatalf("unexpected API error code: %#v", apiErr)
	}
}

func TestListMyFeedback_SendsProjectSlug(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/feedbacks" {
			http.NotFound(w, r)
			return
		}
		query := r.URL.Query()
		if got := query.Get("license_key"); got != "LIC-FEEDBACK-001" {
			t.Fatalf("unexpected license key: %s", got)
		}
		if got := query.Get("project_slug"); got != "demo-project" {
			t.Fatalf("unexpected project slug: %s", got)
		}
		if got := query.Get("user_id"); got != "user-001" {
			t.Fatalf("unexpected user id: %s", got)
		}
		if got := query.Get("page"); got != "2" {
			t.Fatalf("unexpected page: %s", got)
		}
		if got := query.Get("page_size"); got != "25" {
			t.Fatalf("unexpected page_size: %s", got)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{},
			"pagination": map[string]any{
				"total":     0,
				"page":      2,
				"page_size": 25,
			},
		})
	}))
	defer srv.Close()

	guard := newFeedbackTestGuard(t, srv.URL)
	resp, err := guard.ListMyFeedback(context.Background(), "user-001", 2, 25)
	if err != nil {
		t.Fatalf("list feedback: %v", err)
	}
	if resp.PageNum() != 2 || resp.Size() != 25 || resp.Total() != 0 {
		t.Fatalf("unexpected pagination: %#v", resp.Pagination)
	}
}

func TestFetchReleaseNotes_WorkerReleasesShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/feedbacks/release-notes" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("project_slug"); got != "demo-project" {
			t.Fatalf("unexpected project slug query: %s", got)
		}
		if got := r.URL.Query().Get("license_key"); got != "LIC-FEEDBACK-001" {
			t.Fatalf("unexpected license key query: %s", got)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"releases": []map[string]any{
				{
					"component_slug": "backend",
					"version":        "1.2.3",
					"release_notes":  "Fixed startup crash",
					"feedbacks": []map[string]any{
						{
							"id":       "fb-1",
							"title":    "Startup crash",
							"category": "bug",
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	guard := newFeedbackTestGuard(t, srv.URL)
	notes, err := guard.FetchReleaseNotes(context.Background())
	if err != nil {
		t.Fatalf("fetch release notes: %v", err)
	}
	if len(notes.Entries) != 1 {
		t.Fatalf("expected 1 release note entry, got %d", len(notes.Entries))
	}
	entry := notes.Entries[0]
	if entry.ComponentSlug != "backend" || entry.Version != "1.2.3" || entry.ReleaseNotes != "Fixed startup crash" {
		t.Fatalf("unexpected release note entry: %#v", entry)
	}
	if len(entry.ResolvedFeedbacks) != 1 || entry.ResolvedFeedbacks[0].ID != "fb-1" {
		t.Fatalf("unexpected feedback mapping: %#v", entry.ResolvedFeedbacks)
	}
}

func TestFetchReleaseNotes_LegacyEntriesShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/feedbacks/release-notes" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("license_key"); got != "LIC-FEEDBACK-001" {
			t.Fatalf("unexpected license key query: %s", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"entries": []map[string]any{
				{
					"version":       "1.0.0",
					"release_notes": "Initial release",
				},
			},
		})
	}))
	defer srv.Close()

	guard := newFeedbackTestGuard(t, srv.URL)
	notes, err := guard.FetchReleaseNotes(context.Background())
	if err != nil {
		t.Fatalf("fetch legacy release notes: %v", err)
	}
	if len(notes.Entries) != 1 || notes.Entries[0].Version != "1.0.0" {
		t.Fatalf("unexpected legacy release notes: %#v", notes.Entries)
	}
}
