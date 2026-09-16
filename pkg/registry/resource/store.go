package resource

import (
	"context"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// ObjectStore is the persistence contract the generic resource handlers and
// the batch apply pipeline drive for one kind. *v1alpha1store.Store is the
// production implementation; an extension kind may supply its own, for
// example a store that owns its transaction boundary or that authorizes
// inside the store. A store that denies returns auth.ErrForbidden or
// auth.ErrUnauthenticated (wrapped or bare) and the handlers map those to
// 403 / 401 the same way the Authorize hook would.
type ObjectStore interface {
	Get(ctx context.Context, namespace, name, tag string) (*v1alpha1.RawObject, error)
	GetLatest(ctx context.Context, namespace, name string) (*v1alpha1.RawObject, error)
	GetLatestIncludingTerminating(ctx context.Context, namespace, name string) (*v1alpha1.RawObject, error)
	ListTags(ctx context.Context, namespace, name string) ([]*v1alpha1.RawObject, error)
	List(ctx context.Context, opts v1alpha1store.ListOpts) ([]*v1alpha1.RawObject, string, error)
	Upsert(ctx context.Context, obj v1alpha1.Object, opts ...v1alpha1store.UpsertOpts) (v1alpha1store.UpsertResult, error)
	DeleteByRef(ctx context.Context, namespace, name, tag string) error
}

var _ ObjectStore = (*v1alpha1store.Store)(nil)
