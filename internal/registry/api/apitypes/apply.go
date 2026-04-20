package apitypes

// ApplyResult describes the outcome for a single document in a
// multi-doc apply or delete batch. Returned by POST /v0/apply and
// DELETE /v0/apply in the Body.Results slice.
//
// Clients outside the handler package (Go client, CLI) depend on this
// shape, so it lives in apitypes to avoid an internal/internal import.
type ApplyResult struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
	Version    string `json:"version,omitempty"`
	// Status is one of: created, configured, unchanged, deleted,
	// dry-run, failed. Matches kubectl-style apply output.
	Status string `json:"status"`
	// Generation is the server-managed generation after the apply.
	// Zero for failed, dry-run, or deleted results.
	Generation int64 `json:"generation,omitempty"`
	// Error is the failure detail for Status=="failed".
	Error string `json:"error,omitempty"`
}

// ApplyStatus* are the well-known Status values on ApplyResult.
const (
	ApplyStatusCreated    = "created"
	ApplyStatusConfigured = "configured"
	ApplyStatusUnchanged  = "unchanged"
	ApplyStatusDeleted    = "deleted"
	ApplyStatusDryRun     = "dry-run"
	ApplyStatusFailed     = "failed"
)

// ApplyResultsResponse is the response envelope body for POST/DELETE
// /v0/apply. Wrapped once here so Huma OpenAPI output + Go client +
// CLI all agree on the outer shape.
type ApplyResultsResponse struct {
	Results []ApplyResult `json:"results"`
}
