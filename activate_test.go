package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type activateCtxKey string

func TestActivate_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/activate" {
			json.NewEncoder(w).Encode(ActivationResult{
				LicenseKey:  "XXXXX-XXXXX-XXXXX-XXXXX",
				ProjectSlug: "test-project",
				ExpiresAt:   "2025-12-31T23:59:59Z",
			})
		}
	}))
	defer server.Close()

	result, err := Activate(server.URL, "CDK-TEST-CODE", "Acme Corp", "[email protected]")
	if err != nil {
		t.Fatalf("Activate failed: %v", err)
	}

	if result.LicenseKey == "" {
		t.Error("expected license key, got empty string")
	}

	if result.ProjectSlug != "test-project" {
		t.Errorf("expected project_slug test-project, got %s", result.ProjectSlug)
	}
}

func TestActivate_CDKNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "cdk_not_found",
		})
	}))
	defer server.Close()

	_, err := Activate(server.URL, "INVALID-CODE", "Acme Corp", "[email protected]")
	if !errors.Is(err, ErrCDKNotFound) {
		t.Errorf("expected ErrCDKNotFound, got %v", err)
	}
}

func TestActivate_CDKAlreadyUsed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "cdk_already_used",
		})
	}))
	defer server.Close()

	_, err := Activate(server.URL, "USED-CODE", "Acme Corp", "[email protected]")
	if !errors.Is(err, ErrCDKAlreadyUsed) {
		t.Errorf("expected ErrCDKAlreadyUsed, got %v", err)
	}
}

func TestActivate_CDKRevoked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "cdk_revoked",
		})
	}))
	defer server.Close()

	_, err := Activate(server.URL, "REVOKED-CODE", "Acme Corp", "[email protected]")
	if !errors.Is(err, ErrCDKRevoked) {
		t.Errorf("expected ErrCDKRevoked, got %v", err)
	}
}

func TestActivate_MissingParameters(t *testing.T) {
	tests := []struct {
		name         string
		serverURL    string
		code         string
		organization string
		expectedErr  string
	}{
		// Note: empty serverURL now uses DefaultServerURL instead of returning error
		{"missing code", "http://localhost", "", "Org", "activation code is required"},
		{"missing organization", "http://localhost", "CODE", "", "organization is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Activate(tt.serverURL, tt.code, tt.organization, "")
			if err == nil {
				t.Error("expected error, got nil")
			}
			if err.Error() != tt.expectedErr {
				t.Errorf("expected error %q, got %q", tt.expectedErr, err.Error())
			}
		})
	}
}

func TestActivate_InvalidServerURL(t *testing.T) {
	_, err := Activate("url", "CDK-TEST-CODE", "Acme Corp", "")
	if !errors.Is(err, ErrInvalidServerURL) {
		t.Fatalf("expected ErrInvalidServerURL, got %v", err)
	}
}

func TestActivateWithOptions_UsesContextAndUserAgent(t *testing.T) {
	ctx := context.WithValue(context.Background(), activateCtxKey("trace"), "activate-flow")
	result, err := ActivateWithOptions(ActivationOptions{
		ServerURL:    "http://guard.example.com",
		Code:         "CDK-TEST-CODE",
		Organization: "Acme Corp",
		Context:      ctx,
		UserAgent:    "custom-agent/1.0",
		HTTPClient: &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				if got := req.Header.Get("User-Agent"); got != "custom-agent/1.0" {
					t.Fatalf("unexpected user agent: %q", got)
				}
				if req.Context().Value(activateCtxKey("trace")) != "activate-flow" {
					t.Fatalf("expected context value to flow into request")
				}
				body, err := json.Marshal(ActivationResult{LicenseKey: "key"})
				if err != nil {
					t.Fatalf("marshal activation response: %v", err)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("ActivateWithOptions failed: %v", err)
	}
	if result.LicenseKey != "key" {
		t.Fatalf("unexpected license key: %q", result.LicenseKey)
	}
}

func TestActivateWithOptions_NormalizesServerURLTrailingSlash(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/activate" {
			t.Fatalf("unexpected activation path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(ActivationResult{LicenseKey: "key"})
	}))
	defer server.Close()

	result, err := ActivateWithOptions(ActivationOptions{
		ServerURL:        server.URL + "/",
		Code:             "CDK-TEST-CODE",
		Organization:     "Acme Corp",
		AllowSystemTrust: true,
	})
	if err != nil {
		t.Fatalf("ActivateWithOptions failed: %v", err)
	}
	if result.LicenseKey != "key" {
		t.Fatalf("unexpected license key: %q", result.LicenseKey)
	}
}

func TestActivateWithOptions_RequiresPinsForHTTPSByDefault(t *testing.T) {
	_, err := ActivateWithOptions(ActivationOptions{
		ServerURL:    DefaultServerURL,
		Code:         "CDK-TEST-CODE",
		Organization: "Acme Corp",
	})
	if !errors.Is(err, ErrTLSPinNotConfigured) {
		t.Fatalf("expected ErrTLSPinNotConfigured, got %v", err)
	}
}

func TestActivateWithOptions_PreservesStructuredAPIErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":   "cdk_not_found",
			"message": "Activation code not found",
		})
	}))
	defer server.Close()

	_, err := ActivateWithOptions(ActivationOptions{
		ServerURL:        server.URL,
		Code:             "MISSING-CODE",
		Organization:     "Acme Corp",
		AllowSystemTrust: true,
	})
	if err == nil {
		t.Fatal("expected activation error")
	}
	if !errors.Is(err, ErrCDKNotFound) {
		t.Fatalf("expected ErrCDKNotFound, got %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Code != "cdk_not_found" || apiErr.Message != "Activation code not found" {
		t.Fatalf("unexpected API error payload: %#v", apiErr)
	}
}
