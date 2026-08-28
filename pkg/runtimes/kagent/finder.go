package kagent

import (
	"context"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// NewStoreDeploymentFinder returns a DeploymentFinderFunc backed by a Deployment
// store. Discovered Deployments are ignored; a missing Deployment returns false.
func NewStoreDeploymentFinder(store *v1alpha1store.Store) DeploymentFinderFunc {
	return func(
		ctx context.Context,
		targetRef v1alpha1.ResourceRef,
		runtimeRef v1alpha1.ResourceRef,
	) (*v1alpha1.Deployment, bool, error) {
		if store == nil {
			return nil, false, fmt.Errorf("deployment store is not configured")
		}
		rows, _, err := store.List(ctx, v1alpha1store.ListOpts{
			Limit: 1,
			ExtraWhere: "spec->'targetRef'->>'kind' = $1 AND " +
				"spec->'targetRef'->>'name' = $2 AND " +
				"COALESCE(NULLIF(spec->'targetRef'->>'namespace', ''), namespace) = $3 AND " +
				"COALESCE(NULLIF(spec->'targetRef'->>'tag', ''), 'latest') = " +
				"COALESCE(NULLIF($4, ''), 'latest') AND " +
				"spec->'runtimeRef'->>'name' = $5 AND " +
				"COALESCE(NULLIF(spec->'runtimeRef'->>'namespace', ''), namespace) = $6 AND " +
				"COALESCE(annotations->>'agentregistry.solo.io/origin', 'managed') <> 'discovered'",
			ExtraArgs: []any{
				targetRef.Kind,
				targetRef.Name,
				targetRef.Namespace,
				targetRef.Tag,
				runtimeRef.Name,
				runtimeRef.Namespace,
			},
		})
		if err != nil {
			return nil, false, fmt.Errorf(
				"find Deployment for %s %s/%s: %w",
				targetRef.Kind,
				targetRef.Namespace,
				targetRef.Name,
				err,
			)
		}
		if len(rows) == 0 {
			return nil, false, nil
		}
		deployment, err := v1alpha1.EnvelopeFromRaw(func() *v1alpha1.Deployment {
			return &v1alpha1.Deployment{}
		}, rows[0], v1alpha1.KindDeployment)
		if err != nil {
			return nil, false, fmt.Errorf(
				"decode Deployment %s/%s: %w",
				targetRef.Namespace,
				rows[0].Metadata.Name,
				err,
			)
		}
		return deployment, true, nil
	}
}
