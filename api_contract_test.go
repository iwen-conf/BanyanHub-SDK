package sdk

import (
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"testing"
)

var sdkErrorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type sdkAPIContract struct {
	OpenAPI      string                             `json:"openapi"`
	Paths        map[string]map[string]sdkOperation `json:"paths"`
	Components   sdkComponents                      `json:"components"`
	XSDKErrorMap map[string]string                  `json:"x-sdk-error-map"`
}

type sdkComponents struct {
	Schemas map[string]json.RawMessage `json:"schemas"`
}

type sdkOperation struct {
	OperationID    string                 `json:"operationId"`
	XSDKErrorCodes []string               `json:"x-sdk-error-codes"`
	Responses      map[string]sdkResponse `json:"responses"`
}

type sdkSchema struct {
	Ref                  string               `json:"$ref"`
	Type                 string               `json:"type"`
	Nullable             bool                 `json:"nullable"`
	Required             []string             `json:"required"`
	Items                *sdkSchema           `json:"items"`
	Properties           map[string]sdkSchema `json:"properties"`
	AdditionalProperties json.RawMessage      `json:"additionalProperties"`
}

type sdkResponse struct {
	Content map[string]sdkMediaType `json:"content"`
}

type sdkMediaType struct {
	Schema sdkSchema `json:"schema"`
}

func TestSDKAPIContract(t *testing.T) {
	data, err := os.ReadFile("contract/sdk-api.v1.json")
	if err != nil {
		t.Fatalf("failed to read SDK API contract: %v", err)
	}

	var contract sdkAPIContract
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatalf("failed to unmarshal SDK API contract: %v", err)
	}
	if contract.OpenAPI == "" {
		t.Fatal("contract openapi version is required")
	}

	expectedOperations := map[string]map[string][]string{
		"/api/v1/activate": {
			"post": {"invalid_request", "cdk_not_found", "cdk_already_used", "cdk_revoked", "project_not_found", "license_creation_failed", "internal_error", "server_misconfigured"},
		},
		"/api/v1/verify": {
			"post": {"invalid_request", "timestamp_expired", "nonce_reused", "license_not_found", "project_not_found", "project_not_authorized", "binary_not_recognized", "invalid_license_signature", "license_inactive", "license_expired", "max_machines_exceeded", "machine_banned", "internal_error", "server_misconfigured"},
		},
		"/api/v1/heartbeat": {
			"post": {"invalid_request", "timestamp_expired", "nonce_reused", "license_not_found", "project_not_found", "binary_not_recognized", "machine_not_registered", "machine_banned", "lease_revoked", "internal_error", "server_misconfigured"},
		},
		"/api/v1/version/resolve": {
			"post": {"invalid_request", "license_invalid", "license_inactive", "machine_banned", "version_not_found", "internal_error"},
		},
		"/api/v1/update/download": {
			"post": {"invalid_request", "license_invalid", "project_not_found", "project_not_authorized", "update_frozen", "machine_invalid", "component_not_found", "artifact_not_found", "artifact_missing_from_storage", "internal_error"},
		},
		"/api/v1/update/fetch/{token}": {
			"get": {"missing_params", "download_token_invalid_or_expired", "artifact_missing", "not_found"},
		},
		"/api/v1/plugins/catalog": {
			"get": {"invalid_request", "license_invalid", "project_not_found", "project_not_authorized", "machine_invalid"},
		},
		"/api/v1/plugins/{slug}/update": {
			"post": {"missing_plugin_slug", "invalid_request", "license_invalid", "project_not_found", "project_not_authorized", "machine_invalid", "update_frozen", "plugin_not_found", "ota_disabled", "artifact_not_found", "artifact_missing_from_storage", "internal_error"},
		},
		"/api/v1/feedbacks": {
			"get":  {"missing_license_key", "missing_project_slug", "missing_user_id", "license_invalid"},
			"post": {"invalid_request", "license_invalid", "machine_invalid", "internal_error"},
		},
		"/api/v1/feedbacks/upload-url": {
			"post": {"invalid_request", "license_invalid"},
		},
		"/api/v1/feedbacks/upload": {
			"post": {"invalid_form_data", "missing_license_key", "missing_project_slug", "missing_file_key", "missing_file", "invalid_file_key", "license_invalid"},
		},
		"/api/v1/feedbacks/release-notes": {
			"get": {"missing_license_key", "missing_project_slug", "license_invalid"},
		},
		"/api/v1/marketplace/browse": {
			"get": {"invalid_request"},
		},
		"/api/v1/marketplace/{slug}": {
			"get": {"missing_slug", "not_found"},
		},
		"/api/v1/marketplace/{slug}/reviews": {
			"get": {"missing_slug", "invalid_request", "not_found"},
		},
		"/api/v1/marketplace/{slug}/install": {
			"post": {"missing_slug", "invalid_request", "license_invalid", "project_not_found", "project_not_authorized", "machine_invalid", "not_found", "incompatible_platform", "artifact_missing_from_storage", "internal_error"},
		},
		"/api/v1/marketplace/{slug}/uninstall": {
			"post": {"missing_slug", "invalid_request", "license_invalid", "project_not_found", "project_not_authorized", "machine_invalid", "not_found", "not_installed"},
		},
		"/api/v1/marketplace/{slug}/configure": {
			"post": {"missing_slug", "invalid_request", "license_invalid", "project_not_found", "project_not_authorized", "machine_invalid", "not_found", "config_validation_failed", "not_installed"},
		},
		"/api/v1/marketplace/{slug}/status": {
			"post": {"missing_slug", "invalid_request", "license_invalid", "project_not_found", "project_not_authorized", "machine_invalid", "not_found", "not_installed"},
		},
		"/api/v1/marketplace/{slug}/review": {
			"post": {"missing_slug", "invalid_request", "license_invalid", "project_not_found", "project_not_authorized", "machine_invalid", "not_found", "install_required", "internal_error"},
		},
	}

	for path, methods := range expectedOperations {
		contractMethods, ok := contract.Paths[path]
		if !ok {
			t.Fatalf("contract missing path %s", path)
		}
		for method, errorCodes := range methods {
			operation, ok := contractMethods[method]
			if !ok {
				t.Fatalf("contract missing operation %s %s", method, path)
			}
			if operation.OperationID == "" {
				t.Fatalf("contract operation %s %s missing operationId", method, path)
			}
			assertSuccessResponsesHaveSchemas(t, method, path, operation)
			for _, code := range operation.XSDKErrorCodes {
				if !sdkErrorCodePattern.MatchString(code) {
					t.Fatalf("contract operation %s %s has non-snake_case error code %q", method, path, code)
				}
				if contract.XSDKErrorMap[code] == "" {
					t.Fatalf("contract operation %s %s declares unmapped SDK error code %q", method, path, code)
				}
			}
			for _, code := range errorCodes {
				if !containsString(operation.XSDKErrorCodes, code) {
					t.Fatalf("contract operation %s %s missing error code %q", method, path, code)
				}
			}
		}
	}

	requiredSchemas := []string{
		"APIError",
		"ActivateRequest",
		"ActivateResponse",
		"VerifyRequest",
		"VerifyResponse",
		"Lease",
		"HeartbeatRequest",
		"HeartbeatResponse",
		"HeartbeatUpdate",
		"UpdateDownloadRequest",
		"UpdateDownloadResponse",
		"PluginCatalog",
		"PluginInfo",
		"PluginUpdatePackage",
		"FeedbackAttachment",
		"FeedbackItem",
		"FeedbackAttachmentInfo",
		"FeedbackReply",
		"FeedbackListPagination",
		"FeedbackListResponse",
		"UploadURLResponse",
		"ResolvedFeedback",
		"ReleaseNoteEntry",
		"ReleaseNotesResponse",
		"MarketplaceItem",
		"MarketplaceItemStats",
		"MarketplaceCatalog",
		"MarketplaceInstallState",
		"MarketplaceDetail",
		"MarketplaceReview",
		"MarketplaceReviewList",
		"MarketplaceInstallPackage",
		"MarketplaceReviewSubmitResult",
		"MarketplaceActionResponse",
	}
	for _, schema := range requiredSchemas {
		if _, ok := contract.Components.Schemas[schema]; !ok {
			t.Fatalf("contract missing schema %s", schema)
		}
	}

	submitFeedback, ok := contract.Paths["/api/v1/feedbacks"]["post"]
	if !ok {
		t.Fatal("contract missing submitFeedback operation")
	}
	if _, ok := submitFeedback.Responses["201"]; !ok {
		t.Fatal("submitFeedback must document the Worker's 201 Created response")
	}
	if _, ok := submitFeedback.Responses["200"]; ok {
		t.Fatal("submitFeedback should not document 200; Worker returns 201 Created")
	}

	assertNoWeakSchemas(t, contract)
	assertMarketplaceItemNullableFields(t, contract)
	assertPluginSchemasMatchWorkerResponses(t, contract)

	heartbeatResponse := mustSchema(t, contract, "HeartbeatResponse")
	updatesSchema, ok := heartbeatResponse.Properties["updates"]
	if !ok {
		t.Fatal("HeartbeatResponse missing updates property")
	}
	if updatesSchema.Type != "array" {
		t.Fatalf("HeartbeatResponse.updates type = %q, want array", updatesSchema.Type)
	}
	if updatesSchema.Items == nil || updatesSchema.Items.Ref != "#/components/schemas/HeartbeatUpdate" {
		t.Fatalf("HeartbeatResponse.updates items ref = %#v, want HeartbeatUpdate ref", updatesSchema.Items)
	}

	heartbeatUpdate := mustSchema(t, contract, "HeartbeatUpdate")
	expectedHeartbeatUpdateFields := map[string]string{
		"component":        "string",
		"current":          "string",
		"latest":           "string",
		"update_available": "boolean",
		"mandatory":        "boolean",
		"release_notes":    "string",
	}
	for field, fieldType := range expectedHeartbeatUpdateFields {
		if !containsString(heartbeatUpdate.Required, field) {
			t.Fatalf("HeartbeatUpdate missing required field %q", field)
		}
		property, ok := heartbeatUpdate.Properties[field]
		if !ok {
			t.Fatalf("HeartbeatUpdate missing property %q", field)
		}
		if property.Type != fieldType {
			t.Fatalf("HeartbeatUpdate.%s type = %q, want %q", field, property.Type, fieldType)
		}
	}

	requiredErrorMappings := map[string]string{
		"artifact_missing":                  "ErrUpdateDownload",
		"artifact_missing_from_storage":     "ErrUpdateDownload",
		"artifact_not_found":                "ErrUpdateDownload",
		"binary_not_recognized":             "ErrBinaryNotRecognized",
		"cdk_already_used":                  "ErrCDKAlreadyUsed",
		"cdk_not_found":                     "ErrCDKNotFound",
		"cdk_revoked":                       "ErrCDKRevoked",
		"component_not_found":               "ErrComponentNotFound",
		"config_validation_failed":          "ErrMarketplaceConfigInvalid",
		"download_token_invalid_or_expired": "ErrUpdateDownload",
		"incompatible_platform":             "ErrMarketplaceIncompatible",
		"install_required":                  "ErrMarketplaceInstallRequired",
		"internal_error":                    "ErrInvalidServerResponse",
		"invalid_file_key":                  "ErrUploadInvalid",
		"invalid_form_data":                 "ErrUploadInvalid",
		"invalid_license_signature":         "ErrLicenseInvalid",
		"invalid_request":                   "ErrInvalidRequest",
		"lease_revoked":                     "ErrLeaseRevoked",
		"license_creation_failed":           "ErrLicenseCreationFailed",
		"license_expired":                   "ErrLicenseExpired",
		"license_inactive":                  "ErrLicenseInvalid",
		"license_invalid":                   "ErrLicenseInvalid",
		"license_not_found":                 "ErrLicenseInvalid",
		"license_revoked":                   "ErrLicenseInvalid",
		"license_suspended":                 "ErrLicenseSuspended",
		"machine_banned":                    "ErrMachineBanned",
		"machine_invalid":                   "ErrMachineBanned",
		"machine_not_registered":            "ErrMachineNotRegistered",
		"max_machines_exceeded":             "ErrMaxMachinesExceeded",
		"missing_file":                      "ErrMissingParameter",
		"missing_file_key":                  "ErrMissingParameter",
		"missing_license_key":               "ErrMissingParameter",
		"missing_params":                    "ErrMissingParameter",
		"missing_plugin_slug":               "ErrMissingParameter",
		"missing_project_slug":              "ErrMissingParameter",
		"missing_slug":                      "ErrMissingParameter",
		"missing_user_id":                   "ErrMissingParameter",
		"nonce_reused":                      "ErrNonceReused",
		"not_found":                         "ErrNotFound",
		"not_installed":                     "ErrMarketplaceNotInstalled",
		"ota_disabled":                      "ErrPluginOTADisabled",
		"plugin_not_found":                  "ErrPluginNotFound",
		"project_not_authorized":            "ErrProjectNotAuthorized",
		"project_not_found":                 "ErrProjectNotFound",
		"server_misconfigured":              "ErrInvalidServerResponse",
		"timestamp_expired":                 "ErrTimestampExpired",
		"update_frozen":                     "ErrUpdateFrozen",
		"version_not_found":                 "ErrNotFound",
	}
	sentinelErrors := map[string]error{
		"ErrBinaryNotRecognized":        ErrBinaryNotRecognized,
		"ErrCDKAlreadyUsed":             ErrCDKAlreadyUsed,
		"ErrCDKNotFound":                ErrCDKNotFound,
		"ErrCDKRevoked":                 ErrCDKRevoked,
		"ErrComponentNotFound":          ErrComponentNotFound,
		"ErrInvalidRequest":             ErrInvalidRequest,
		"ErrInvalidServerResponse":      ErrInvalidServerResponse,
		"ErrLeaseRevoked":               ErrLeaseRevoked,
		"ErrLicenseCreationFailed":      ErrLicenseCreationFailed,
		"ErrLicenseExpired":             ErrLicenseExpired,
		"ErrLicenseInvalid":             ErrLicenseInvalid,
		"ErrLicenseSuspended":           ErrLicenseSuspended,
		"ErrMachineBanned":              ErrMachineBanned,
		"ErrMachineNotRegistered":       ErrMachineNotRegistered,
		"ErrMarketplaceConfigInvalid":   ErrMarketplaceConfigInvalid,
		"ErrMarketplaceIncompatible":    ErrMarketplaceIncompatible,
		"ErrMarketplaceInstallRequired": ErrMarketplaceInstallRequired,
		"ErrMarketplaceNotInstalled":    ErrMarketplaceNotInstalled,
		"ErrMaxMachinesExceeded":        ErrMaxMachinesExceeded,
		"ErrMissingParameter":           ErrMissingParameter,
		"ErrNonceReused":                ErrNonceReused,
		"ErrNotFound":                   ErrNotFound,
		"ErrPluginNotFound":             ErrPluginNotFound,
		"ErrPluginOTADisabled":          ErrPluginOTADisabled,
		"ErrProjectNotAuthorized":       ErrProjectNotAuthorized,
		"ErrProjectNotFound":            ErrProjectNotFound,
		"ErrTimestampExpired":           ErrTimestampExpired,
		"ErrUpdateDownload":             ErrUpdateDownload,
		"ErrUpdateFrozen":               ErrUpdateFrozen,
		"ErrUploadInvalid":              ErrUploadInvalid,
	}
	for code, sentinel := range contract.XSDKErrorMap {
		if !sdkErrorCodePattern.MatchString(code) {
			t.Fatalf("contract error map has non-snake_case error code %q", code)
		}
		if sentinel == "" {
			t.Fatalf("contract error map %s has empty sentinel", code)
		}
		if requiredErrorMappings[code] == "" {
			t.Fatalf("contract error map %s is not covered by requiredErrorMappings", code)
		}
		sentinelErr, ok := sentinelErrors[sentinel]
		if !ok {
			t.Fatalf("contract error map %s references unknown sentinel %q", code, sentinel)
		}
		if got := sdkErrorForAPIErrorCode(code, 400); !errors.Is(got, sentinelErr) {
			t.Fatalf("sdkErrorForAPIErrorCode(%q) = %v, want %s", code, got, sentinel)
		}
	}
	for code, sentinel := range requiredErrorMappings {
		if got := contract.XSDKErrorMap[code]; got != sentinel {
			t.Fatalf("contract error map %s = %q, want %q", code, got, sentinel)
		}
	}
}

func assertPluginSchemasMatchWorkerResponses(t *testing.T, contract sdkAPIContract) {
	t.Helper()

	updateDownload := mustSchema(t, contract, "UpdateDownloadResponse")
	assertRequiredFields(t, "UpdateDownloadResponse", updateDownload, []string{
		"download_url",
		"sha256",
		"signature",
		"size_bytes",
		"expires_in",
	})

	pluginCatalog := mustSchema(t, contract, "PluginCatalog")
	assertRequiredFields(t, "PluginCatalog", pluginCatalog, []string{
		"project_slug",
		"machine_id",
		"source_os",
		"source_arch",
		"update_frozen",
		"plugins",
	})

	pluginUpdate := mustSchema(t, contract, "PluginUpdatePackage")
	for _, field := range []string{"current_version", "release_notes"} {
		property, ok := pluginUpdate.Properties[field]
		if !ok {
			t.Fatalf("PluginUpdatePackage missing property %q", field)
		}
		if !property.Nullable {
			t.Fatalf("PluginUpdatePackage.%s must be nullable to match Worker update response", field)
		}
	}
}

func assertMarketplaceItemNullableFields(t *testing.T, contract sdkAPIContract) {
	t.Helper()

	marketplaceItem := mustSchema(t, contract, "MarketplaceItem")
	for _, field := range []string{"id", "component_id", "template_id", "thumbnail_key", "screenshots"} {
		if containsString(marketplaceItem.Required, field) {
			t.Fatalf("MarketplaceItem must not require internal/raw field %q", field)
		}
		if _, ok := marketplaceItem.Properties[field]; ok {
			t.Fatalf("MarketplaceItem must not expose internal/raw field %q", field)
		}
	}
	for _, field := range []string{"thumbnail_url", "screenshot_urls"} {
		if _, ok := marketplaceItem.Properties[field]; !ok {
			t.Fatalf("MarketplaceItem missing public media field %q", field)
		}
	}
	for _, field := range []string{"manifest", "os", "arch", "config_schema"} {
		property, ok := marketplaceItem.Properties[field]
		if !ok {
			t.Fatalf("MarketplaceItem missing property %q", field)
		}
		if !property.Nullable {
			t.Fatalf("MarketplaceItem.%s must be nullable to match Worker normalizeItem output", field)
		}
	}
}

func assertRequiredFields(t *testing.T, schemaName string, schema sdkSchema, fields []string) {
	t.Helper()

	for _, field := range fields {
		if !containsString(schema.Required, field) {
			t.Fatalf("%s missing required field %q", schemaName, field)
		}
	}
}

func assertSuccessResponsesHaveSchemas(t *testing.T, method, path string, operation sdkOperation) {
	t.Helper()

	for status, response := range operation.Responses {
		if status == "default" || status < "200" || status >= "300" {
			continue
		}
		if len(response.Content) == 0 {
			t.Fatalf("contract operation %s %s response %s missing content schema", method, path, status)
		}
		for mediaType, content := range response.Content {
			if content.Schema.Type == "" && content.Schema.Ref == "" {
				t.Fatalf("contract operation %s %s response %s media %s missing schema type/ref", method, path, status, mediaType)
			}
		}
	}
}

func assertNoWeakSchemas(t *testing.T, contract sdkAPIContract) {
	t.Helper()

	for name, raw := range contract.Components.Schemas {
		var schema sdkSchema
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("failed to unmarshal schema %s: %v", name, err)
		}
		assertSchemaFullySpecified(t, "schemas."+name, schema)
	}
}

func assertSchemaFullySpecified(t *testing.T, path string, schema sdkSchema) {
	t.Helper()

	if schema.Type == "array" && schema.Items == nil {
		t.Fatalf("%s is an array schema without items", path)
	}
	if schema.Type == "object" && len(schema.Properties) == 0 && len(schema.AdditionalProperties) == 0 {
		t.Fatalf("%s is an object schema without properties or additionalProperties", path)
	}
	for name, property := range schema.Properties {
		assertSchemaFullySpecified(t, path+".properties."+name, property)
	}
	if schema.Items != nil {
		assertSchemaFullySpecified(t, path+".items", *schema.Items)
	}
}

func mustSchema(t *testing.T, contract sdkAPIContract, name string) sdkSchema {
	t.Helper()

	raw, ok := contract.Components.Schemas[name]
	if !ok {
		t.Fatalf("contract missing schema %s", name)
	}
	var schema sdkSchema
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("failed to unmarshal schema %s: %v", name, err)
	}
	return schema
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
