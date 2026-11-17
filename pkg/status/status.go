package status

const (
	// ConditionTypeReady reports whether reconciliation reached its desired state.
	ConditionTypeReady = "Ready"
	// ConditionReasonFailed reports that reconciliation reached a terminal failure.
	ConditionReasonFailed = "Failed"
)
