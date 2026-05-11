package common

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

const (
	// DefaultWaitTimeout is used when WaitOptions.Timeout is zero.
	DefaultWaitTimeout = 5 * time.Minute

	defaultPollInterval = 2 * time.Second
)

// WaitOptions configures WaitForDeployment.
//
// Timeout regimes:
//   - > 0: wait at most this long.
//   - == 0: poll once and return.
//   - < 0: wait forever.
type WaitOptions struct {
	// TargetStatus is the deployment status the wait succeeds on
	// (e.g. "deployed", "failed"). Ignored when TargetDeleted is true.
	// Defaults to "deployed" when both fields are zero.
	TargetStatus string

	// TargetDeleted, when true, waits for the deployment to disappear
	// (the resolver returns a not-found error). Mutually exclusive
	// with TargetStatus.
	TargetDeleted bool

	// Timeout caps the total wait. See type doc for the three regimes.
	Timeout time.Duration

	// PollInterval is the delay between poll attempts. Zero uses
	// defaultPollInterval (2s).
	PollInterval time.Duration

	// Progress is called once after each non-terminal poll with the
	// observed status and elapsed time.
	Progress func(status string, elapsed time.Duration)
}

// ResolveDeploymentFunc fetches the current deployment record.
// Implementations must return a not-found error (database.ErrNotFound,
// client.ErrNotFound, or any error wrapping either) when the deployment
// no longer exists.
type ResolveDeploymentFunc func(ctx context.Context) (*DeploymentRecord, error)

// WaitForDeployment polls until the deployment reaches the requested state,
// reaches a different terminal state, or the timeout is exceeded.
//
// With TargetStatus set, the wait succeeds on dep.Status == TargetStatus
// and fails on any other terminal state (deployed / failed / undeployed).
// With TargetDeleted, the wait succeeds on a not-found error and keeps
// polling otherwise. Context cancellation surfaces as ctx.Err().
func WaitForDeployment(ctx context.Context, resolve ResolveDeploymentFunc, opts WaitOptions) error {
	if resolve == nil {
		return errors.New("WaitForDeployment: resolve function is nil")
	}

	target := opts.TargetStatus
	if !opts.TargetDeleted && target == "" {
		target = "deployed"
	}

	pollInterval := opts.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultPollInterval
	}

	start := time.Now()
	for {
		dep, err := resolve(ctx)
		notFound := isDeploymentNotFound(err)
		if err != nil && !notFound {
			return fmt.Errorf("polling deployment status: %w", err)
		}

		switch {
		case notFound && opts.TargetDeleted:
			return nil
		case notFound:
			return errors.New("deployment not found")
		case !opts.TargetDeleted && dep.Status == target:
			return nil
		case !opts.TargetDeleted && isTerminalStatus(dep.Status):
			if dep.Error != "" {
				return fmt.Errorf("deployment reached state %q (waiting for %q): %s",
					dep.Status, target, dep.Error)
			}
			return fmt.Errorf("deployment reached state %q (waiting for %q)",
				dep.Status, target)
		}

		observed := "deleted"
		if dep != nil {
			observed = dep.Status
		}
		elapsed := time.Since(start)

		if opts.Timeout == 0 || (opts.Timeout > 0 && elapsed >= opts.Timeout) {
			return fmt.Errorf("timed out after %s waiting for deployment to reach %q (current status: %s)",
				elapsed.Round(time.Second), targetDescription(opts), observed)
		}

		if opts.Progress != nil {
			opts.Progress(observed, elapsed)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func isTerminalStatus(s string) bool {
	switch s {
	case "deployed", "failed", "undeployed":
		return true
	}
	return false
}

func targetDescription(opts WaitOptions) string {
	if opts.TargetDeleted {
		return "deleted"
	}
	if opts.TargetStatus == "" {
		return "deployed"
	}
	return opts.TargetStatus
}

func isDeploymentNotFound(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, database.ErrNotFound) || errors.Is(err, client.ErrNotFound)
}
