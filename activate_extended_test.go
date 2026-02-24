package sdk

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestActivate_InvalidParameters tests activation with missing parameters
func TestActivate_InvalidParameters(t *testing.T) {
	tests := []struct {
		name      string
		serverURL string
		code      string
		org       string
		email     string
	}{
		{"missing code", "http://localhost", "", "org", "email@test.com"},
		{"missing org", "http://localhost", "code123", "", "email@test.com"},
		{"missing email", "http://localhost", "code123", "org", ""},
		{"missing serverURL", "", "code123", "org", "email@test.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Activate(tt.serverURL, tt.code, tt.org, tt.email)
			if err == nil {
				t.Error("expected error for invalid parameters")
			}
		})
	}
}

// TestActivate_SuccessWithAllFields tests successful activation with all fields
func TestActivate_SuccessWithAllFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/activate" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"license_key":   "activated-license-key",
				"project_slug":  "test-project",
				"expires_at":    "2027-02-23T00:00:00Z",
			})
		}
	}))

	result, err := Activate(server.URL, "code123", "org", "email@test.com")
	if err != nil {
		t.Fatalf("Activate failed: %v", err)
	}

	if result.LicenseKey != "activated-license-key" {
		t.Errorf("expected license key activated-license-key, got %s", result.LicenseKey)
	}
	if result.ProjectSlug != "test-project" {
		t.Errorf("expected project slug test-project, got %s", result.ProjectSlug)
	}
	if result.ExpiresAt != "2027-02-23T00:00:00Z" {
		t.Errorf("expected expires_at 2027-02-23T00:00:00Z, got %s", result.ExpiresAt)
	}

	server.Close()
}

// TestActivate_NetworkError tests with network failure
func TestActivate_NetworkError(t *testing.T) {
	_, err := Activate("http://invalid-server.local", "code123", "org", "email@test.com")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// TestActivate_ServerError tests with server error response
func TestActivate_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "internal_server_error",
		})
	}))

	_, err := Activate(server.URL, "code123", "org", "email@test.com")
	if err == nil {
		t.Error("expected error, got nil")
	}

	server.Close()
}

// TestActivate_InvalidJSON tests with invalid JSON response
func TestActivate_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("invalid json"))
	}))

	_, err := Activate(server.URL, "code123", "org", "email@test.com")
	if err == nil {
		t.Error("expected error, got nil")
	}

	server.Close()
}

// TestActivate_WithMinimalParameters tests activation with minimal required parameters
func TestActivate_WithMinimalParameters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/activate" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"license_key": "key",
			})
		}
	}))

	result, err := Activate(server.URL, "code", "o", "e@t.com")
	if err != nil {
		t.Fatalf("Activate failed: %v", err)
	}

	if result.LicenseKey != "key" {
		t.Errorf("expected license key key, got %s", result.LicenseKey)
	}

	server.Close()
}

// TestActivate_EmptyResponse tests with empty response fields
func TestActivate_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/activate" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"license_key":  "key",
				"project_slug": "",
				"expires_at":   "",
			})
		}
	}))

	result, err := Activate(server.URL, "code", "org", "email@test.com")
	if err != nil {
		t.Fatalf("Activate failed: %v", err)
	}

	if result.LicenseKey != "key" {
		t.Errorf("expected license key, got %s", result.LicenseKey)
	}

	server.Close()
}
