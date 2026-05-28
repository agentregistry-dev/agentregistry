package controller

import (
	"context"
	"log/slog"
	"time"

	"istio.io/istio/pkg/kube/krt"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// DeploymentWorkDeriver turns projected source state into durable work. It
// deliberately does not call adapters; side effects begin only after a work
// claim is leased.
type DeploymentWorkDeriver struct {
	Sources *SourceIndex
	Work    *v1alpha1store.ReconcileWorkStore
	Intents krt.Collection[DeploymentWorkIntent]
	Now     func() time.Time
}

// RegisterKRTHandlers wires the leaf side effect to the KRT-derived Deployment
// work-intent graph. KRT owns dependency tracking; this handler only persists
// durable work for changed intents.
func (d *DeploymentWorkDeriver) RegisterKRTHandlers(ctx context.Context) []krt.HandlerRegistration {
	if d == nil {
		return nil
	}
	intents := d.workIntents()
	if intents == nil {
		return nil
	}
	return []krt.HandlerRegistration{
		intents.RegisterBatch(func(events []krt.Event[DeploymentWorkIntent]) {
			if _, err := d.DeriveIntentEvents(ctx, events); err != nil {
				slog.Error("deployment controller derivation failed", "error", err)
			}
		}, false),
	}
}

// DeriveAll is the repair/bootstrap path. Incremental work should flow through
// the DeploymentWorkIntent collection so dependency changes rederive only
// affected Deployments.
func (d *DeploymentWorkDeriver) DeriveAll(ctx context.Context) (int, error) {
	if d == nil {
		return 0, nil
	}
	intents := d.workIntents()
	if intents == nil {
		return 0, nil
	}
	if !intents.WaitUntilSynced(ctx.Done()) {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		return 0, ErrProjectorNotReady
	}
	count := 0
	for _, intent := range intents.List() {
		if err := d.UpsertIntent(ctx, intent); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (d *DeploymentWorkDeriver) DeriveIntentEvents(ctx context.Context, events []krt.Event[DeploymentWorkIntent]) (int, error) {
	count := 0
	for _, event := range events {
		if event.New == nil {
			continue
		}
		if err := d.UpsertIntent(ctx, *event.New); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (d *DeploymentWorkDeriver) UpsertIntent(ctx context.Context, intent DeploymentWorkIntent) error {
	if d == nil || d.Work == nil {
		return nil
	}
	work := intent.Work
	if work.NextAttemptAt.IsZero() {
		work.NextAttemptAt = d.now()
	}
	if err := d.Work.Upsert(ctx, work); err != nil {
		return err
	}
	_, err := d.Work.AbandonSuperseded(ctx, work)
	return err
}

func (d *DeploymentWorkDeriver) workIntents() krt.Collection[DeploymentWorkIntent] {
	if d == nil {
		return nil
	}
	if d.Intents != nil {
		return d.Intents
	}
	if d.Sources == nil {
		return nil
	}
	d.Intents = NewDeploymentWorkIntents(d.Sources)
	return d.Intents
}

func (d *DeploymentWorkDeriver) now() time.Time {
	if d != nil && d.Now != nil {
		return d.Now().UTC()
	}
	return time.Now().UTC()
}
