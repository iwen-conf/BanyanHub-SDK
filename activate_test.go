package sdk

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
	if err != ErrCDKNotFound {
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
	if err != ErrCDKAlreadyUsed {
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
	if err != ErrCDKRevoked {
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
		{"missing server URL", "", "CODE", "Org", "server_url is required"},
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
