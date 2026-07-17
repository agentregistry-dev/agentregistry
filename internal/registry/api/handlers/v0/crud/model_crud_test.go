//go:build integration

package crud_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/crud"
	arv0 "github.com/agentregistry-dev/agentregistry/pkg/api/v0"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

func TestModelCRUD(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	stores := v1alpha1store.NewStores(pool, v1alpha1store.TestSchemaRegistry())
	_, api := humatest.New(t)
	crud.Register(api, "/v0", stores, nil, nil, crud.PerKindHooks{}, nil)
	resource.RegisterApply(api, resource.ApplyConfig{BasePrefix: "/v0", Stores: stores})

	applyModel := func(model v1alpha1.Model) arv0.ApplyResult {
		t.Helper()
		doc, err := yaml.Marshal(model)
		require.NoError(t, err)
		resp := api.Post("/v0/apply", "Content-Type: application/yaml", strings.NewReader(string(doc)))
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		var out arv0.ApplyResultsResponse
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
		require.Len(t, out.Results, 1)
		require.NotEqual(t, arv0.ApplyStatusFailed, out.Results[0].Status, out.Results[0].Error)
		return out.Results[0]
	}

	model := v1alpha1.Model{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindModel},
		Metadata: v1alpha1.ObjectMeta{Name: "claude-opus"},
		Spec: v1alpha1.ModelSpec{
			Provider: v1alpha1.ModelProviderBedrock,
			Model:    "us.anthropic.claude-opus-4-8",
			Auth:     &v1alpha1.ModelAuthConfig{Strategy: v1alpha1.ModelAuthStrategyRuntime},
		},
	}

	result := applyModel(model)
	require.Equal(t, v1alpha1store.DefaultTag(), result.Tag)

	resp := api.Get("/v0/models/claude-opus/latest")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var created v1alpha1.Model
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &created))
	require.Equal(t, v1alpha1.ModelProviderBedrock, created.Spec.Provider)
	require.NotEmpty(t, created.Metadata.UID)
	require.Equal(t, v1alpha1store.DefaultTag(), created.Metadata.Tag)

	resp = api.Get("/v0/models/claude-opus")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var got v1alpha1.Model
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	require.Equal(t, created.Spec, got.Spec)

	model.Metadata.Tag = "approved-v2"
	model.Spec.Endpoint = &v1alpha1.ModelEndpointConfig{Region: "us-west-2"}
	result = applyModel(model)
	require.Equal(t, "approved-v2", result.Tag)

	resp = api.Get("/v0/models/claude-opus/approved-v2")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var updated v1alpha1.Model
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &updated))
	require.Equal(t, "us-west-2", updated.Spec.Endpoint.Region)
	require.NotEqual(t, created.Metadata.UID, updated.Metadata.UID)

	// The name-only route remains pinned to the literal latest tag; publishing
	// another tag does not silently change existing unpinned lookups.
	resp = api.Get("/v0/models/claude-opus")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	require.Nil(t, got.Spec.Endpoint)

	resp = api.Get("/v0/models")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var list struct {
		Items []v1alpha1.Model `json:"items"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &list))
	require.Len(t, list.Items, 2)
	require.Equal(t, "claude-opus", list.Items[0].Metadata.Name)

	resp = api.Get("/v0/models/claude-opus/tags")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var tags struct {
		Items []v1alpha1.Model `json:"items"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &tags))
	require.Len(t, tags.Items, 2)

	resp = api.Delete("/v0/models/claude-opus/approved-v2")
	require.Equal(t, http.StatusNoContent, resp.Code, resp.Body.String())
	resp = api.Get("/v0/models/claude-opus/approved-v2")
	require.Equal(t, http.StatusNotFound, resp.Code, resp.Body.String())
	resp = api.Get("/v0/models/claude-opus")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
}
