//go:build integration

package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/resource"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// TestClient_V1Alpha1RoundTrip exercises the new generic client methods
// (Apply / Get / GetLatest / List / Delete) against a real v1alpha1
// resource handler backed by a test DB. Proves the wire contract end
// to end and pins the shape the CLI + UI regen will consume.
func TestClient_V1Alpha1RoundTrip(t *testing.T) {
	pool := database.NewV1Alpha1TestPool(t)
	stores := database.NewV1Alpha1Stores(pool)

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("test", "v1"))
	resource.RegisterBuiltins(api, "/v0", stores, nil, nil, nil, resource.DeploymentHooks{})
	resource.RegisterApply(api, resource.ApplyConfig{
		BasePrefix: "/v0",
		Stores:     stores,
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	c := client.NewClient(ts.URL, "")
	ctx := context.Background()

	// Apply a single-doc YAML → creates the Agent.
	yamlBody := []byte(`
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  namespace: default
  name: acme/planner
  version: v1.0.0
spec:
  title: Planner
  description: planning agent
`)
	results, err := c.Apply(ctx, yamlBody, client.ApplyOpts{})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "created", results[0].Status)
	require.EqualValues(t, 1, results[0].Generation)

	// Get by exact version.
	raw, err := c.Get(ctx, v1alpha1.KindAgent, "default", "acme/planner", "v1.0.0")
	require.NoError(t, err)
	require.Equal(t, v1alpha1.KindAgent, raw.Kind)
	require.Equal(t, "acme/planner", raw.Metadata.Name)
	require.Equal(t, "v1.0.0", raw.Metadata.Version)
	require.EqualValues(t, 1, raw.Metadata.Generation)

	// Unmarshal Spec into the typed Agent.
	var spec v1alpha1.AgentSpec
	require.NoError(t, json.Unmarshal(raw.Spec, &spec))
	require.Equal(t, "Planner", spec.Title)

	// GetLatest returns the same row.
	latest, err := c.GetLatest(ctx, v1alpha1.KindAgent, "default", "acme/planner")
	require.NoError(t, err)
	require.Equal(t, "v1.0.0", latest.Metadata.Version)

	// List (cross-namespace) returns the one row.
	items, next, err := c.List(ctx, v1alpha1.KindAgent, client.ListOpts{})
	require.NoError(t, err)
	require.Equal(t, "", next)
	require.Len(t, items, 1)
	require.Equal(t, "acme/planner", items[0].Metadata.Name)

	// List (namespaced) returns the same.
	items, _, err = c.List(ctx, v1alpha1.KindAgent, client.ListOpts{Namespace: "default"})
	require.NoError(t, err)
	require.Len(t, items, 1)

	// Delete → row flips to terminating (soft-delete). GetLatest stops
	// seeing it because recomputeLatest excludes terminating rows; the
	// exact-version Get still returns the row with deletionTimestamp set.
	require.NoError(t, c.Delete(ctx, v1alpha1.KindAgent, "default", "acme/planner", "v1.0.0"))

	raw, err = c.Get(ctx, v1alpha1.KindAgent, "default", "acme/planner", "v1.0.0")
	require.NoError(t, err)
	require.NotNil(t, raw.Metadata.DeletionTimestamp)

	_, err = c.GetLatest(ctx, v1alpha1.KindAgent, "default", "acme/planner")
	require.ErrorIs(t, err, client.ErrNotFound)
}

// TestClient_V1Alpha1_ApplyInvalid covers the apply pipeline's
// per-document failure branch at the client level.
func TestClient_V1Alpha1_ApplyInvalid(t *testing.T) {
	pool := database.NewV1Alpha1TestPool(t)
	stores := database.NewV1Alpha1Stores(pool)

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("test", "v1"))
	resource.RegisterApply(api, resource.ApplyConfig{
		BasePrefix: "/v0",
		Stores:     stores,
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	c := client.NewClient(ts.URL, "")

	// Missing required metadata.name — apply handler validates + reports
	// a per-document failed result; Apply() returns no transport-level
	// error.
	yamlBody := []byte(`
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  namespace: default
  version: v1.0.0
spec:
  title: Missing name
`)
	results, err := c.Apply(context.Background(), yamlBody, client.ApplyOpts{})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "failed", results[0].Status)
	require.Contains(t, results[0].Error, "metadata.name")
}

// TestClient_V1Alpha1_NotFound proves the ErrNotFound sentinel path.
func TestClient_V1Alpha1_NotFound(t *testing.T) {
	pool := database.NewV1Alpha1TestPool(t)
	stores := database.NewV1Alpha1Stores(pool)

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("test", "v1"))
	resource.RegisterBuiltins(api, "/v0", stores, nil, nil, nil, resource.DeploymentHooks{})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	c := client.NewClient(ts.URL, "")

	_, err := c.Get(context.Background(), v1alpha1.KindAgent, "default", "does-not-exist", "v1")
	require.Error(t, err)
	require.True(t, errors.Is(err, client.ErrNotFound))
}
