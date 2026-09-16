//go:build unit

package resource_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"

	arv0 "github.com/agentregistry-dev/agentregistry/pkg/api/v0"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// memoryStore is a minimal ObjectStore for a mutable kind: rows keyed by
// namespace/name, no tags, no soft delete. deny, when set, is returned by
// every method to exercise the store-level access mapping.
type memoryStore struct {
	mu   sync.Mutex
	rows map[string]*v1alpha1.RawObject
	deny error
}

func newMemoryStore() *memoryStore {
	return &memoryStore{rows: map[string]*v1alpha1.RawObject{}}
}

func rowKey(namespace, name string) string { return namespace + "/" + name }

func (m *memoryStore) Get(ctx context.Context, namespace, name, _ string) (*v1alpha1.RawObject, error) {
	return m.GetLatest(ctx, namespace, name)
}

func (m *memoryStore) GetLatest(_ context.Context, namespace, name string) (*v1alpha1.RawObject, error) {
	if m.deny != nil {
		return nil, m.deny
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[rowKey(namespace, name)]
	if !ok {
		return nil, pkgdb.ErrNotFound
	}
	return row, nil
}

func (m *memoryStore) GetLatestIncludingTerminating(ctx context.Context, namespace, name string) (*v1alpha1.RawObject, error) {
	return m.GetLatest(ctx, namespace, name)
}

func (m *memoryStore) ListTags(context.Context, string, string) ([]*v1alpha1.RawObject, error) {
	return nil, nil
}

func (m *memoryStore) List(_ context.Context, opts v1alpha1store.ListOpts) ([]*v1alpha1.RawObject, string, error) {
	if m.deny != nil {
		return nil, "", m.deny
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*v1alpha1.RawObject, 0, len(m.rows))
	for _, row := range m.rows {
		if opts.Namespace == "" || row.Metadata.Namespace == opts.Namespace {
			out = append(out, row)
		}
	}
	slices.SortFunc(out, func(a, b *v1alpha1.RawObject) int {
		return strings.Compare(rowKey(a.Metadata.Namespace, a.Metadata.Name), rowKey(b.Metadata.Namespace, b.Metadata.Name))
	})
	return out, "", nil
}

func (m *memoryStore) Upsert(_ context.Context, obj v1alpha1.Object, _ ...v1alpha1store.UpsertOpts) (v1alpha1store.UpsertResult, error) {
	if m.deny != nil {
		return v1alpha1store.UpsertResult{}, m.deny
	}
	spec, err := obj.MarshalSpec()
	if err != nil {
		return v1alpha1store.UpsertResult{}, err
	}
	meta := *obj.GetMetadata()
	m.mu.Lock()
	defer m.mu.Unlock()
	k := rowKey(meta.Namespace, meta.Name)
	outcome := v1alpha1store.UpsertCreated
	meta.Generation = 1
	meta.UID = fmt.Sprintf("uid-%d", len(m.rows)+1)
	if existing, ok := m.rows[k]; ok {
		outcome = v1alpha1store.UpsertReplaced
		meta.Generation = existing.Metadata.Generation + 1
		meta.UID = existing.Metadata.UID
	}
	m.rows[k] = &v1alpha1.RawObject{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: obj.GetKind()},
		Metadata: meta,
		Spec:     spec,
	}
	return v1alpha1store.UpsertResult{UID: meta.UID, Generation: meta.Generation, Outcome: outcome}, nil
}

func (m *memoryStore) DeleteByRef(_ context.Context, namespace, name, _ string) error {
	if m.deny != nil {
		return m.deny
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	k := rowKey(namespace, name)
	if _, ok := m.rows[k]; !ok {
		return pkgdb.ErrNotFound
	}
	delete(m.rows, k)
	return nil
}

func newSecretAPI(t *testing.T, store resource.ObjectStore) humatest.TestAPI {
	t.Helper()
	_, api := humatest.New(t)
	resource.Register(api, resource.Config{Kind: v1alpha1.KindSecret, BasePrefix: "/v0", Store: store},
		func() *v1alpha1.Secret { return &v1alpha1.Secret{} })
	resource.RegisterApply(api, resource.ApplyConfig{
		BasePrefix: "/v0",
		Stores:     map[string]resource.ObjectStore{v1alpha1.KindSecret: store},
	})
	return api
}

const batchSecretYAML = `apiVersion: ar.dev/v1alpha1
kind: Secret
metadata:
  name: batch-creds
spec:
  type: Opaque
  stringData:
    token: shh
`

func secretBody(name string) map[string]any {
	return map[string]any{
		"apiVersion": v1alpha1.GroupVersion,
		"kind":       v1alpha1.KindSecret,
		"metadata":   map[string]any{"name": name},
		"spec":       map[string]any{"type": "Opaque", "stringData": map[string]string{"password": "hunter2"}},
	}
}

// A store that is not *v1alpha1store.Store serves the kind routes and the
// batch endpoint unchanged.
func TestObjectStore_CustomStoreServesRoutesAndBatchApply(t *testing.T) {
	store := newMemoryStore()
	api := newSecretAPI(t, store)

	resp := api.Put("/v0/secrets/db-creds", secretBody("db-creds"))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var stored v1alpha1.Secret
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &stored))
	require.Equal(t, "db-creds", stored.Metadata.Name)

	require.Equal(t, http.StatusOK, api.Get("/v0/secrets/db-creds").Code)
	require.Equal(t, http.StatusNotFound, api.Get("/v0/secrets/missing").Code)

	resp = api.Post("/v0/apply", "Content-Type: application/yaml", strings.NewReader(batchSecretYAML))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var results arv0.ApplyResultsResponse
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &results))
	require.Len(t, results.Results, 1)
	require.Equal(t, arv0.ApplyStatusCreated, results.Results[0].Status, results.Results[0].Error)

	resp = api.Get("/v0/secrets?namespace=all")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var list struct {
		Items []v1alpha1.Secret `json:"items"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &list))
	require.Len(t, list.Items, 2)

	require.Equal(t, http.StatusNoContent, api.Delete("/v0/secrets/db-creds").Code)
	resp = api.Delete("/v0/apply", "Content-Type: application/yaml", strings.NewReader(batchSecretYAML))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &results))
	require.Equal(t, arv0.ApplyStatusDeleted, results.Results[0].Status, results.Results[0].Error)
	require.Empty(t, store.rows)
}

// A store that authorizes inside itself surfaces the same status codes the
// Authorize hook would, on every route and in batch results.
func TestObjectStore_StoreDenialsMapToAccessErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		deny   error
		status int
	}{
		{"forbidden", auth.ErrForbidden, http.StatusForbidden},
		{"unauthenticated", fmt.Errorf("tenant finance: %w", auth.ErrUnauthenticated), http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemoryStore()
			store.deny = tc.deny
			api := newSecretAPI(t, store)
			require.Equal(t, tc.status, api.Get("/v0/secrets/db-creds").Code)
			require.Equal(t, tc.status, api.Get("/v0/secrets").Code)
			require.Equal(t, tc.status, api.Put("/v0/secrets/db-creds", secretBody("db-creds")).Code)
			require.Equal(t, tc.status, api.Delete("/v0/secrets/db-creds").Code)
			resp := api.Post("/v0/apply", "Content-Type: application/yaml", strings.NewReader(batchSecretYAML))
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
			var results arv0.ApplyResultsResponse
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &results))
			require.Equal(t, arv0.ApplyStatusFailed, results.Results[0].Status)
			require.True(t, strings.HasPrefix(results.Results[0].Error, "forbidden: "), results.Results[0].Error)
		})
	}
}
