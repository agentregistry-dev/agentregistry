package config

// autoApprove controls whether pushed/published items are auto-approved
// For OSS, this is true by default. Implementations could call SetAutoApprove(false) to disable auto-approval.
var autoApprove = true

// SetAutoApprove configures whether pushed/published items should be auto-approved.
func SetAutoApprove(enabled bool) {
	autoApprove = enabled
}

// GetAutoApprove returns the current auto-approve setting.
// Internal push/publish commands use this to check if items should be auto-approved.
func GetAutoApprove() bool {
	return autoApprove
}
