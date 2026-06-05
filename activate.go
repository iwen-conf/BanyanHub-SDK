package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const defaultActivationTimeout = 30 * time.Second

// ActivationResult holds the result of a CDK activation.
type ActivationResult struct {
	LicenseKey  string `json:"license_key"`
	ProjectSlug string `json:"project_slug"`
	ExpiresAt   string `json:"expires_at"`
}

// ActivationOptions configures a CDK activation request.
type ActivationOptions struct {
	ServerURL        string
	Code             string
	Organization     string
	Email            string
	Context          context.Context
	Timeout          time.Duration
	HTTPClient       *http.Client
	AllowSystemTrust bool
	PinnedSPKIHashes []string
	UserAgent        string
}

// Activate sends a CDK activation request to the server.
// It exchanges an activation code for a license key.
// If serverURL is empty, DefaultServerURL is used.
func Activate(serverURL, code, organization, email string) (*ActivationResult, error) {
	return ActivateWithOptions(ActivationOptions{
		ServerURL:        serverURL,
		Code:             code,
		Organization:     organization,
		Email:            email,
		Timeout:          defaultActivationTimeout,
		AllowSystemTrust: true,
	})
}

// ActivateWithOptions exchanges an activation code for a license key with
// explicit transport and request controls.
func ActivateWithOptions(opts ActivationOptions) (*ActivationResult, error) {
	if opts.Code == "" {
		return nil, fmt.Errorf("activation code is required")
	}
	if opts.Organization == "" {
		return nil, fmt.Errorf("organization is required")
	}
	serverURL, err := normalizeServerURL(opts.ServerURL)
	if err != nil {
		return nil, err
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultActivationTimeout
	}

	ctx := opts.Context
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
		defer cancel()
	}

	client := opts.HTTPClient
	if client == nil {
		pinnedClient, err := newPinnedHTTPClient(Config{
			ServerURL:        serverURL,
			AllowSystemTrust: opts.AllowSystemTrust,
			PinnedSPKIHashes: opts.PinnedSPKIHashes,
		})
		if err != nil {
			return nil, err
		}
		pinnedClient.Timeout = timeout
		client = pinnedClient
	}

	payload := map[string]string{
		"code":         opts.Code,
		"organization": opts.Organization,
	}
	if opts.Email != "" {
		payload["email"] = opts.Email
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL+"/api/v1/activate", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", activationUserAgent(opts.UserAgent))

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetworkError, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, decodeAPIErrorResponse(resp)
	}

	var result ActivationResult
	if err := decodeAPIJSONResponse(resp, &result); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidServerResponse, err)
	}

	return &result, nil
}

func activationUserAgent(userAgent string) string {
	if userAgent != "" {
		return userAgent
	}
	return "BanyanHub-SDK/" + Version
}
