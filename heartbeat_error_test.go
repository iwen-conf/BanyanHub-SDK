package sdk

import (
	"testing"
)

func TestIsFatalError(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		result bool
	}{
		{"license_suspended", ErrLicenseSuspended, true},
		{"machine_banned", ErrMachineBanned, true},
		{"banned", ErrBanned, true},
		{"license_invalid", ErrLicenseInvalid, false},
		{"license_expired", ErrLicenseExpired, false},
		{"network_error", ErrNetworkError, false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isFatalError(tt.err)
			if result != tt.result {
				t.Errorf("isFatalError(%v) = %v, want %v", tt.err, result, tt.result)
			}
		})
	}
}
