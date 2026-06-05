package sdk

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxAPIErrorBodyBytes = 64 * 1024

// APIError preserves structured server error details for non-2xx SDK API
// responses while still unwrapping to stable SDK sentinel errors.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
	Cause      error
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	code := e.Code
	if code == "" {
		code = "request_failed"
	}
	if e.Message != "" {
		return fmt.Sprintf("api error (%d:%s): %s", e.StatusCode, code, e.Message)
	}
	return fmt.Sprintf("api error (%d:%s)", e.StatusCode, code)
}

func (e *APIError) Unwrap() []error {
	errs := []error{ErrInvalidServerResponse}
	if e != nil && e.Cause != nil && e.Cause != ErrInvalidServerResponse {
		errs = append(errs, e.Cause)
	}
	return errs
}

// IsAPIError reports whether err contains APIError. If code is empty, this
// only checks the error type.
func IsAPIError(err error, code string) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return code == "" || apiErr.Code == code
}

func decodeAPIErrorResponse(resp *http.Response) error {
	type errorEnvelope struct {
		Error   string `json:"error"`
		Reason  string `json:"reason"`
		Message string `json:"message"`
	}

	raw, truncated, readErr := readAPIErrorBody(resp.Body)
	envelope := errorEnvelope{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &envelope)
	}

	code := envelope.Error
	if code == "" {
		code = envelope.Reason
	}
	if code == "" {
		code = "request_failed"
	}

	message := envelope.Message
	if message == "" {
		message = strings.TrimSpace(string(raw))
		if truncated && message != "" {
			message += " [truncated]"
		}
	}
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	if readErr != nil {
		message = fmt.Sprintf("%s [error body read failed: %v]", message, readErr)
	}

	return &APIError{
		StatusCode: resp.StatusCode,
		Code:       code,
		Message:    message,
		Cause:      sdkErrorForAPIErrorCode(code, resp.StatusCode),
	}
}

func readAPIErrorBody(body io.Reader) ([]byte, bool, error) {
	if body == nil {
		return nil, false, nil
	}
	limited := io.LimitReader(body, maxAPIErrorBodyBytes+1)
	raw, err := io.ReadAll(limited)
	if len(raw) > maxAPIErrorBodyBytes {
		return raw[:maxAPIErrorBodyBytes], true, err
	}
	return raw, false, err
}

func sdkErrorForAPIErrorCode(code string, statusCode int) error {
	switch code {
	case "license_not_found", "license_inactive", "license_invalid", "invalid_license_signature", "license_revoked":
		return ErrLicenseInvalid
	case "invalid_request":
		return ErrInvalidRequest
	case "internal_error", "server_misconfigured":
		return ErrInvalidServerResponse
	case "version_not_found", "not_found":
		return ErrNotFound
	case "license_expired":
		return ErrLicenseExpired
	case "license_suspended":
		return ErrLicenseSuspended
	case "project_not_found":
		return ErrProjectNotFound
	case "project_not_authorized":
		return ErrProjectNotAuthorized
	case "max_machines_exceeded":
		return ErrMaxMachinesExceeded
	case "machine_banned", "machine_invalid":
		return ErrMachineBanned
	case "machine_not_registered":
		return ErrMachineNotRegistered
	case "binary_not_recognized":
		return ErrBinaryNotRecognized
	case "timestamp_expired":
		return ErrTimestampExpired
	case "nonce_reused":
		return ErrNonceReused
	case "lease_revoked":
		return ErrLeaseRevoked
	case "update_frozen":
		return ErrUpdateFrozen
	case "component_not_found":
		return ErrComponentNotFound
	case "plugin_not_found":
		return ErrPluginNotFound
	case "missing_params", "missing_plugin_slug", "missing_license_key", "missing_user_id", "missing_project_slug", "missing_slug", "missing_file_key", "missing_file":
		return ErrMissingParameter
	case "ota_disabled":
		return ErrPluginOTADisabled
	case "artifact_not_found", "artifact_missing", "artifact_missing_from_storage", "download_token_invalid_or_expired":
		return ErrUpdateDownload
	case "cdk_not_found":
		return ErrCDKNotFound
	case "cdk_already_used":
		return ErrCDKAlreadyUsed
	case "cdk_revoked":
		return ErrCDKRevoked
	case "license_creation_failed":
		return ErrLicenseCreationFailed
	case "invalid_form_data", "invalid_file_key":
		return ErrUploadInvalid
	case "incompatible_platform":
		return ErrMarketplaceIncompatible
	case "install_required":
		return ErrMarketplaceInstallRequired
	case "not_installed":
		return ErrMarketplaceNotInstalled
	case "config_validation_failed":
		return ErrMarketplaceConfigInvalid
	default:
		if statusCode >= 500 {
			return ErrInvalidServerResponse
		}
		return ErrInvalidServerResponse
	}
}
