package sdk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ActivationResult holds the result of a CDK activation.
type ActivationResult struct {
	LicenseKey  string `json:"license_key"`
	ProjectSlug string `json:"project_slug"`
	ExpiresAt   string `json:"expires_at"`
}

// Activate sends a CDK activation request to the server.
// It exchanges an activation code for a license key.
func Activate(serverURL, code, organization, email string) (*ActivationResult, error) {
	if serverURL == "" {
		return nil, fmt.Errorf("server_url is required")
	}
	if code == "" {
		return nil, fmt.Errorf("activation code is required")
	}
	if organization == "" {
		return nil, fmt.Errorf("organization is required")
	}

	payload := map[string]string{
		"code":         code,
		"organization": organization,
	}
	if email != "" {
		payload["email"] = email
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	url := serverURL + "/api/v1/activate"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetworkError, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)

		switch errResp.Error {
		case "cdk_not_found":
			return nil, ErrCDKNotFound
		case "cdk_already_used":
			return nil, ErrCDKAlreadyUsed
		case "cdk_revoked":
			return nil, ErrCDKRevoked
		default:
			return nil, fmt.Errorf("%w: status %d, error: %s", ErrInvalidServerResponse, resp.StatusCode, errResp.Error)
		}
	}

	var result ActivationResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidServerResponse, err)
	}

	return &result, nil
}
