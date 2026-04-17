package deployment

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrDeploymentDriftIsWrappable(t *testing.T) {
	wrapped := fmt.Errorf("%w: deployment dep-123", ErrDeploymentDrift)
	if !errors.Is(wrapped, ErrDeploymentDrift) {
		t.Fatalf("expected wrapped error to match ErrDeploymentDrift via errors.Is")
	}
}
