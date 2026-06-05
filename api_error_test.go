package sdk

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type failingAPIErrorBody struct{}

func (failingAPIErrorBody) Read(_ []byte) (int, error) {
	return 0, errors.New("forced body read failure")
}

func TestAPIErrorMappingPreservesStructuredDetails(t *testing.T) {
	err := (&APIError{
		StatusCode: http.StatusForbidden,
		Code:       "update_frozen",
		Message:    "Update channel is frozen.",
		Cause:      ErrUpdateFrozen,
	})

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatal("expected errors.As to find APIError")
	}
	if apiErr.StatusCode != http.StatusForbidden || apiErr.Code != "update_frozen" {
		t.Fatalf("unexpected api error details: %#v", apiErr)
	}
	if !errors.Is(err, ErrUpdateFrozen) {
		t.Fatal("expected APIError to unwrap to ErrUpdateFrozen")
	}
	if !errors.Is(err, ErrInvalidServerResponse) {
		t.Fatal("expected APIError to unwrap to ErrInvalidServerResponse")
	}
	if !IsAPIError(err, "update_frozen") {
		t.Fatal("expected IsAPIError to match update_frozen")
	}
}

func TestPostJSONDecodesAPIErrorEnvelope(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":   "update_frozen",
			"message": "Update channel is frozen.",
		})
	}))
	defer srv.Close()

	g, err := New(Config{
		ServerURL:     srv.URL,
		LicenseKey:    "LIC-1",
		PublicKeyPEM:  pemEncodePublicKey(pubKey),
		ProjectSlug:   "project",
		ComponentSlug: "backend",
	})
	if err != nil {
		t.Fatalf("new guard: %v", err)
	}

	err = g.postJSON(context.Background(), "/api/v1/update/download", map[string]string{"x": "y"}, &struct{}{})
	if err == nil {
		t.Fatal("expected API error")
	}
	if !errors.Is(err, ErrUpdateFrozen) {
		t.Fatalf("expected ErrUpdateFrozen, got %v", err)
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusForbidden || apiErr.Code != "update_frozen" || apiErr.Message != "Update channel is frozen." {
		t.Fatalf("unexpected APIError: %#v", apiErr)
	}
}

func TestGetJSONDecodesReasonWhenErrorFieldMissing(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"reason":  "machine_banned",
			"message": "Machine is banned.",
		})
	}))
	defer srv.Close()

	g, err := New(Config{
		ServerURL:     srv.URL,
		LicenseKey:    "LIC-1",
		PublicKeyPEM:  pemEncodePublicKey(pubKey),
		ProjectSlug:   "project",
		ComponentSlug: "backend",
	})
	if err != nil {
		t.Fatalf("new guard: %v", err)
	}

	err = g.getJSON(context.Background(), "/api/v1/plugins/catalog", nil, &struct{}{})
	if err == nil {
		t.Fatal("expected API error")
	}
	if !errors.Is(err, ErrMachineBanned) {
		t.Fatalf("expected ErrMachineBanned, got %v", err)
	}
	if !IsAPIError(err, "machine_banned") {
		t.Fatalf("expected IsAPIError(err, machine_banned)=true")
	}
}

func TestVerifyOnlinePreservesBusinessAPIError(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/verify" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "max_machines_exceeded",
			"max":   1,
		})
	}))
	defer srv.Close()

	g, err := New(Config{
		ServerURL:     srv.URL,
		LicenseKey:    "LIC-1",
		PublicKeyPEM:  pemEncodePublicKey(pubKey),
		ProjectSlug:   "project",
		ComponentSlug: "backend",
	})
	if err != nil {
		t.Fatalf("new guard: %v", err)
	}

	_, _, err = g.verifyOnline(context.Background(), time.Now())
	if err == nil {
		t.Fatal("expected verify API error")
	}
	if !errors.Is(err, ErrMaxMachinesExceeded) {
		t.Fatalf("expected ErrMaxMachinesExceeded, got %v", err)
	}
	if errors.Is(err, ErrNetworkError) {
		t.Fatalf("business API error should not be classified as network error: %v", err)
	}
}

func TestMarketplaceErrorCompatibilityUsesAPIError(t *testing.T) {
	err := (&APIError{
		StatusCode: http.StatusForbidden,
		Code:       "install_required",
		Message:    "Install required.",
		Cause:      sdkErrorForAPIErrorCode("install_required", http.StatusForbidden),
	})

	var marketplaceErr *MarketplaceAPIError
	if !errors.As(err, &marketplaceErr) {
		t.Fatal("expected MarketplaceAPIError alias to match APIError")
	}
	if !IsMarketplaceError(err, "install_required") {
		t.Fatal("expected marketplace compatibility helper to match code")
	}
	if !errors.Is(err, ErrMarketplaceInstallRequired) {
		t.Fatal("expected install_required to unwrap to ErrMarketplaceInstallRequired")
	}
}

func TestDecodeAPIErrorResponseLimitsBodySize(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	body := strings.Repeat("x", maxAPIErrorBodyBytes+512)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	g, err := New(Config{
		ServerURL:     srv.URL,
		LicenseKey:    "LIC-1",
		PublicKeyPEM:  pemEncodePublicKey(pubKey),
		ProjectSlug:   "project",
		ComponentSlug: "backend",
	})
	if err != nil {
		t.Fatalf("new guard: %v", err)
	}

	err = g.getJSON(context.Background(), "/api/v1/plugins/catalog", nil, &struct{}{})
	if err == nil {
		t.Fatal("expected API error")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if len(apiErr.Message) <= maxAPIErrorBodyBytes {
		t.Fatalf("expected truncated suffix in message, got len=%d", len(apiErr.Message))
	}
	if !strings.HasSuffix(apiErr.Message, " [truncated]") {
		t.Fatalf("expected truncated suffix, got %q", apiErr.Message[len(apiErr.Message)-20:])
	}
	if !strings.HasPrefix(apiErr.Message, strings.Repeat("x", 32)) {
		t.Fatalf("expected message to preserve original prefix")
	}
	if len(apiErr.Message) != maxAPIErrorBodyBytes+len(" [truncated]") {
		t.Fatalf("unexpected truncated message len: got %d want %d", len(apiErr.Message), maxAPIErrorBodyBytes+len(" [truncated]"))
	}
}

func TestDecodeAPIErrorResponseReportsBodyReadError(t *testing.T) {
	err := decodeAPIErrorResponse(&http.Response{
		StatusCode: http.StatusBadGateway,
		Body:       io.NopCloser(failingAPIErrorBody{}),
	})

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Code != "request_failed" {
		t.Fatalf("code = %q, want request_failed", apiErr.Code)
	}
	if !strings.Contains(apiErr.Message, http.StatusText(http.StatusBadGateway)) {
		t.Fatalf("message should include status text, got %q", apiErr.Message)
	}
	if !strings.Contains(apiErr.Message, "forced body read failure") {
		t.Fatalf("message should include body read error, got %q", apiErr.Message)
	}
}

func TestPostJSONLimitsSuccessfulResponseBody(t *testing.T) {
	pubKey, _, _ := ed25519.GenerateKey(rand.Reader)
	body := `{"payload":"` + strings.Repeat("x", maxAPIResponseBodyBytes) + `"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	g, err := New(Config{
		ServerURL:     srv.URL,
		LicenseKey:    "LIC-1",
		PublicKeyPEM:  pemEncodePublicKey(pubKey),
		ProjectSlug:   "project",
		ComponentSlug: "backend",
	})
	if err != nil {
		t.Fatalf("new guard: %v", err)
	}

	var result map[string]string
	err = g.postJSON(context.Background(), "/api/v1/heartbeat", map[string]string{"x": "y"}, &result)
	if err == nil {
		t.Fatal("expected oversized response error")
	}
	if !errors.Is(err, ErrInvalidServerResponse) {
		t.Fatalf("expected ErrInvalidServerResponse, got %v", err)
	}
	if !strings.Contains(err.Error(), "response body exceeds") {
		t.Fatalf("expected body size error, got %v", err)
	}
}
