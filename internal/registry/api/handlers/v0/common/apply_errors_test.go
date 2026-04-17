package common_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/common"
	"github.com/agentregistry-dev/agentregistry/internal/registry/kinds"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
)

func TestClassifyNilReturnsApplied(t *testing.T) {
	status, msg := common.ClassifyApplyError(nil)
	if status != kinds.StatusApplied {
		t.Fatalf("expected applied, got %s", status)
	}
	if msg != "" {
		t.Fatalf("expected empty message, got %q", msg)
	}
}

func TestClassifyAnyErrorReturnsFailed(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"drift", fmt.Errorf("%w: deployment dep-1", deployment.ErrDeploymentDrift)},
		{"generic", errors.New("something went wrong")},
		{"wrapped", fmt.Errorf("outer: %w", errors.New("inner"))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, msg := common.ClassifyApplyError(tc.err)
			if status != kinds.StatusFailed {
				t.Fatalf("expected failed, got %s", status)
			}
			if msg == "" {
				t.Fatal("expected non-empty message")
			}
		})
	}
}
