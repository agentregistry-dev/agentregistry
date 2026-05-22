package controller

import (
	"context"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// DeploymentWorkDeriver turns projected source state into durable work. It
// deliberately does not call adapters; side effects begin only after a work
// claim is leased.
type DeploymentWorkDeriver struct {
	Sources *DeploymentSources
	Work    *v1alpha1store.ReconcileWorkStore
	Now     func() time.Time
}

func (d *DeploymentWorkDeriver) DeriveAll(ctx context.Context) (int, error) {
	if d == nil || d.Sources == nil {
		return 0, nil
	}
	count := 0
	for _, row := range d.Sources.DeploymentList() {
		if row.Object == nil {
			continue
		}
		if err := d.DeriveDeployment(ctx, row.Object); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (d *DeploymentWorkDeriver) DeriveDeployment(ctx context.Context, deployment *v1alpha1.Deployment) error {
	if d == nil || d.Work == nil {
		return nil
	}
	work, err := DeriveDeploymentWork(deployment)
	if err != nil {
		return err
	}
	if work.NextAttemptAt.IsZero() {
		work.NextAttemptAt = d.now()
	}
	if d.Sources != nil && (work.Action == ReconcileActionApply || work.Action == ReconcileActionRemove) {
		switch {
		case !d.Sources.RuntimeExists(deployment):
			work.Reason = "runtime-reference-pending"
		case !d.Sources.TargetExists(deployment) && work.Action == ReconcileActionApply:
			work.Reason = "target-reference-pending"
		}
	}
	if err := d.Work.Upsert(ctx, work); err != nil {
		return err
	}
	_, err = d.Work.AbandonSuperseded(ctx, work)
	return err
}

func (d *DeploymentWorkDeriver) now() time.Time {
	if d != nil && d.Now != nil {
		return d.Now().UTC()
	}
	return time.Now().UTC()
}
