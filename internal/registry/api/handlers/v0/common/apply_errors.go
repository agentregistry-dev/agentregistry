// Package common holds cross-kind helpers used by v0 HTTP handlers.
package common

import (
	"github.com/agentregistry-dev/agentregistry/internal/registry/kinds"
)

// ClassifyApplyError maps a service error to a per-document Status and a user-
// facing message. All errors currently become StatusFailed; keeping the Status
// in the return type leaves room for future granularity (e.g. a dedicated Drift
// status, or HTTP-code hints) without changing callers.
func ClassifyApplyError(err error) (kinds.Status, string) {
	if err == nil {
		return kinds.StatusApplied, ""
	}
	return kinds.StatusFailed, err.Error()
}
