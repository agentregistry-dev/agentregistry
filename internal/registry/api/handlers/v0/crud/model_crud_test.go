//go:build integration

package crud_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/crud"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

func TestModelCRUD(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	stores := v1alpha1store.NewStores(pool, v1alpha1store.TestSchemaRegistry())
	_, api := humatest.New(t)
	crud.Register(api, "/v0", stores, nil, nil, crud.PerKindHooks{}, nil)

	model := v1alpha1.Model{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindModel},
		Metadata: v1alpha1.ObjectMeta{Name: "claude-opus"},
		Spec: v1alpha1.ModelSpec{
			Provider: v1alpha1.ModelProviderBedrock,
			Model:    "us.anthropic.claude-opus-4-8",
			Auth:     &v1alpha1.ModelAuthConfig{Strategy: v1alpha1.ModelAuthStrategyRuntime},
		},
	}

	resp := api.Put("/v0/models/claude-opus", model)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var created v1alpha1.Model
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &created))
	require.Equal(t, v1alpha1.ModelProviderBedrock, created.Spec.Provider)
	require.NotEmpty(t, created.Metadata.UID)

	resp = api.Get("/v0/models/claude-opus")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var got v1alpha1.Model
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	require.Equal(t, created.Spec, got.Spec)

	model.Spec.Endpoint = &v1alpha1.ModelEndpointConfig{Region: "us-west-2"}
	resp = api.Put("/v0/models/claude-opus", model)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var updated v1alpha1.Model
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &updated))
	require.Equal(t, "us-west-2", updated.Spec.Endpoint.Region)
	require.Equal(t, created.Metadata.UID, updated.Metadata.UID)

	resp = api.Get("/v0/models")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var list struct {
		Items []v1alpha1.Model `json:"items"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &list))
	require.Len(t, list.Items, 1)
	require.Equal(t, "claude-opus", list.Items[0].Metadata.Name)

	resp = api.Delete("/v0/models/claude-opus")
	require.Equal(t, http.StatusNoContent, resp.Code, resp.Body.String())
	resp = api.Get("/v0/models/claude-opus")
	require.Equal(t, http.StatusNotFound, resp.Code, resp.Body.String())
}
