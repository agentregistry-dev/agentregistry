package resource

import (
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/require"
)

// TestPrepareErrorStatus preserves caller-facing refusals from prepare hooks.
func TestPrepareErrorStatus(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusConflict, http.StatusUnprocessableEntity} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			refusal := huma.NewError(status, "spec.runtimeRef is immutable; delete and recreate the RuntimeAccessPolicy")
			err := mapApplyErrorToHuma(&applyError{Stage: stagePrepare, Err: refusal}, "RuntimeAccessPolicy", "default", "policy", "")
			var statusErr huma.StatusError
			require.ErrorAs(t, err, &statusErr)
			require.Equal(t, status, statusErr.GetStatus())
			require.Same(t, refusal, err)
		})
	}
}
