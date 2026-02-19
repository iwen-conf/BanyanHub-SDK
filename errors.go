package sdk

import "errors"

var (
	ErrLicenseInvalid        = errors.New("license invalid")
	ErrLicenseExpired        = errors.New("license expired")
	ErrLicenseSuspended      = errors.New("license suspended")
	ErrMachineBanned         = errors.New("machine banned")
	ErrMaxMachinesExceeded   = errors.New("max machines exceeded")
	ErrProjectNotAuthorized  = errors.New("project not authorized")
	ErrUpdateFrozen          = errors.New("update channel frozen")
	ErrNetworkError          = errors.New("network error")
	ErrInvalidServerResponse = errors.New("invalid server response")
	ErrNotActivated          = errors.New("guard not activated")
	ErrLocked                = errors.New("system locked: offline grace period expired")
	ErrBanned                = errors.New("system banned")
	ErrCDKNotFound   = errors.New("activation code not found")
	ErrCDKAlreadyUsed = errors.New("activation code already used")
	ErrCDKRevoked    = errors.New("activation code revoked")
)
