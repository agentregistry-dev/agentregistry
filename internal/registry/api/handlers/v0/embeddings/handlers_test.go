//go:build integration

package embeddings

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"

	pkgemb "github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// denyAdminAuthzProvider refuses IsRegistryAdmin so tests can assert
// the admin gate fires.
type denyAdminAuthzProvider struct{}

func (denyAdminAuthzProvider) Check(context.Context, auth.Session, auth.PermissionAction, auth.Resource) error {
	return nil
}
func (denyAdminAuthzProvider) IsRegistryAdmin(context.Context, auth.Session) bool {
	return false
}

// fakeProvider returns a deterministic vector so tests don't need
// network access and can assert progress + result shape precisely.
type fakeProvider struct {
	calls int
}

func (f *fakeProvider) Generate(ctx context.Context, p pkgemb.Payload) (*pkgemb.Result, error) {
	f.calls++
	vec := make([]float32, 1536)
	vec[0] = 1
	return &pkgemb.Result{
		Vector:     vec,
		Provider:   "fake",
		Model:      "fake-small",
		Dimensions: 1536,
	}, nil
}

func seedAgent(t *testing.T, store *v1alpha1store.Store, name string) {
	t.Helper()
	spec, err := json.Marshal(v1alpha1.AgentSpec{Title: name, Description: name})
	require.NoError(t, err)
	_, err = store.Upsert(context.Background(), "default", name, "v1", spec, v1alpha1store.UpsertOpts{})
	require.NoError(t, err)
}

func setupHandlerFixture(t *testing.T) (humatest.TestAPI, *v1alpha1store.Store) {
	t.Helper()
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	seedAgent(t, store, "one")
	seedAgent(t, store, "two")

	provider := &fakeProvider{}
	bindings := []pkgemb.KindBinding{{
		Kind:  v1alpha1.KindAgent,
		Store: store,
		BuildPayload: func(row *v1alpha1.RawObject) (string, error) {
			var spec v1alpha1.AgentSpec
			if err := json.Unmarshal(row.Spec, &spec); err != nil {
				return "", err
			}
			return pkgemb.BuildAgentEmbeddingPayload(row.Metadata, spec), nil
		},
	}}
	indexer, err := pkgemb.NewIndexer(pkgemb.IndexerConfig{
		Bindings:   bindings,
		Provider:   provider,
		Dimensions: 1536,
	})
	require.NoError(t, err)

	_, api := humatest.New(t)
	Register(api, Config{
		BasePrefix: "/v0",
		Indexer:    indexer,
	})
	return api, store
}

func TestHandler_StartIndexJob_ReturnsJobID(t *testing.T) {
	api, _ := setupHandlerFixture(t)

	resp := api.Post("/v0/embeddings/index", strings.NewReader(`{}`))
	require.Equal(t, 200, resp.Code, resp.Body.String())

	var body IndexJobResponse
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	require.NotEmpty(t, body.JobID)
	require.Equal(t, "pending", body.Status)
}

func TestHandler_GetJobStatus_ReportsCompletion(t *testing.T) {
	api, store := setupHandlerFixture(t)

	resp := api.Post("/v0/embeddings/index", strings.NewReader(`{}`))
	require.Equal(t, 200, resp.Code)
	var start IndexJobResponse
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &start))

	// Poll until terminal — background goroutine writes into the
	// manager asynchronously.
	deadline := time.Now().Add(3 * time.Second)
	var status JobStatusResponse
	for time.Now().Before(deadline) {
		got := api.Get("/v0/embeddings/index/" + start.JobID)
		require.Equal(t, 200, got.Code)
		require.NoError(t, json.Unmarshal(got.Body.Bytes(), &status))
		if status.Status == "completed" || status.Status == "failed" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	require.Equal(t, "completed", status.Status, "result=%+v", status.Result)
	require.NotNil(t, status.Result)
	perAgent := status.Result.PerKind[v1alpha1.KindAgent]
	require.Equal(t, 2, perAgent.Processed)
	require.Equal(t, 2, perAgent.Updated)

	// Second run against the same rows — checksums match → everything skipped.
	resp2 := api.Post("/v0/embeddings/index", strings.NewReader(`{}`))
	require.Equal(t, 200, resp2.Code)
	var start2 IndexJobResponse
	require.NoError(t, json.Unmarshal(resp2.Body.Bytes(), &start2))

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got := api.Get("/v0/embeddings/index/" + start2.JobID)
		require.NoError(t, json.Unmarshal(got.Body.Bytes(), &status))
		if status.Status == "completed" || status.Status == "failed" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	require.Equal(t, "completed", status.Status)
	require.Equal(t, 2, status.Result.PerKind[v1alpha1.KindAgent].Skipped)

	// Row-side sanity check: embeddings persisted.
	meta, err := store.GetEmbeddingMetadata(context.Background(), "default", "one", "v1")
	require.NoError(t, err)
	require.NotNil(t, meta)
	require.Equal(t, "fake", meta.Provider)
}

func TestHandler_ConflictWhenJobAlreadyRunning(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	for i := 0; i < 50; i++ {
		seedAgent(t, store, "row"+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}

	// Provider that blocks on each call so we can assert concurrent
	// start returns 409.
	blocker := make(chan struct{})
	sp := slowProvider{block: blocker}
	indexer, err := pkgemb.NewIndexer(pkgemb.IndexerConfig{
		Bindings: []pkgemb.KindBinding{{
			Kind:         v1alpha1.KindAgent,
			Store:        store,
			BuildPayload: func(row *v1alpha1.RawObject) (string, error) { return row.Metadata.Name, nil },
		}},
		Provider:   &sp,
		Dimensions: 1536,
	})
	require.NoError(t, err)

	_, api := humatest.New(t)
	Register(api, Config{BasePrefix: "/v0", Indexer: indexer})

	first := api.Post("/v0/embeddings/index", strings.NewReader(`{}`))
	require.Equal(t, 200, first.Code)

	// Poll until the first job transitions out of pending so Manager
	// reports it as running.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var start IndexJobResponse
		_ = json.Unmarshal(first.Body.Bytes(), &start)
		got := api.Get("/v0/embeddings/index/" + start.JobID)
		var st JobStatusResponse
		_ = json.Unmarshal(got.Body.Bytes(), &st)
		if st.Status == "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	second := api.Post("/v0/embeddings/index", strings.NewReader(`{}`))
	require.Equal(t, 409, second.Code, second.Body.String())

	close(blocker) // let the first job drain so the test cleans up
}

type slowProvider struct {
	block chan struct{}
}

func (s *slowProvider) Generate(ctx context.Context, p pkgemb.Payload) (*pkgemb.Result, error) {
	select {
	case <-s.block:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	vec := make([]float32, 1536)
	return &pkgemb.Result{Vector: vec, Provider: "slow", Model: "x", Dimensions: 1536}, nil
}

func TestHandler_JobNotFound(t *testing.T) {
	api, _ := setupHandlerFixture(t)
	resp := api.Get("/v0/embeddings/index/nope")
	require.Equal(t, 404, resp.Code)
}

func TestHandler_NonAdmin_Forbidden(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")
	seedAgent(t, store, "one")

	indexer, err := pkgemb.NewIndexer(pkgemb.IndexerConfig{
		Bindings: []pkgemb.KindBinding{{
			Kind:         v1alpha1.KindAgent,
			Store:        store,
			BuildPayload: func(row *v1alpha1.RawObject) (string, error) { return row.Metadata.Name, nil },
		}},
		Provider:   &fakeProvider{},
		Dimensions: 1536,
	})
	require.NoError(t, err)

	_, api := humatest.New(t)
	Register(api, Config{
		BasePrefix: "/v0",
		Indexer:    indexer,
		Authz:      auth.Authorizer{Authz: denyAdminAuthzProvider{}},
	})

	// POST is gated.
	resp := api.Post("/v0/embeddings/index", strings.NewReader(`{}`))
	require.Equal(t, 403, resp.Code, resp.Body.String())

	// GET is also gated (prevents job-existence leaks).
	resp = api.Get("/v0/embeddings/index/anything")
	require.Equal(t, 403, resp.Code, resp.Body.String())
}
