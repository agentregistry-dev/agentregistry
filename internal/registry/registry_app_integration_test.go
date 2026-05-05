//go:build integration

package registry

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"

	arv0 "github.com/agentregistry-dev/agentregistry/pkg/api/v0"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

const extensionApplyKind = "IntegrationExtension"

type extensionApplySpec struct {
	Value string `json:"value" yaml:"value"`
}

type extensionApplyObject struct {
	v1alpha1.TypeMeta `json:",inline" yaml:",inline"`
	Metadata          v1alpha1.ObjectMeta `json:"metadata" yaml:"metadata"`
	Spec              extensionApplySpec  `json:"spec" yaml:"spec"`
	Status            v1alpha1.Status     `json:"status,omitzero" yaml:"status,omitempty"`
}

func (e *extensionApplyObject) GetMetadata() *v1alpha1.ObjectMeta    { return &e.Metadata }
func (e *extensionApplyObject) SetMetadata(meta v1alpha1.ObjectMeta) { e.Metadata = meta }
func (e *extensionApplyObject) MarshalSpec() (json.RawMessage, error) {
	return json.Marshal(e.Spec)
}
func (e *extensionApplyObject) UnmarshalSpec(data json.RawMessage) error {
	return json.Unmarshal(data, &e.Spec)
}
func (e *extensionApplyObject) MarshalStatus() (json.RawMessage, error) {
	return v1alpha1.MarshalStatusForStorage(e.Status)
}
func (e *extensionApplyObject) UnmarshalStatus(data json.RawMessage) error {
	return v1alpha1.UnmarshalStatusFromStorage(data, &e.Status)
}

func TestBuildStoresAndImporter_ExtensionKindAppliesThroughBatchEndpoint(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	stores, importer := buildStoresAndImporter(pool, nil, map[string]string{
		extensionApplyKind: "v1alpha1.agents",
	})
	require.NotNil(t, importer)
	extensionStore := stores[extensionApplyKind]
	require.NotNil(t, extensionStore)

	scheme := v1alpha1.NewScheme()
	scheme.MustRegister(extensionApplyKind, extensionApplySpec{}, func() any { return &extensionApplyObject{} })

	_, api := humatest.New(t)
	resource.RegisterApply(api, resource.ApplyConfig{
		BasePrefix: "/v0",
		Stores:     stores,
		Scheme:     scheme,
	})

	yaml := []byte(`apiVersion: ar.dev/v1alpha1
kind: IntegrationExtension
metadata:
  name: enterprise-only
spec:
  value: ok
`)
	resp := api.Post("/v0/apply", "Content-Type: application/yaml", strings.NewReader(string(yaml)))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var out struct {
		Results []arv0.ApplyResult `json:"results"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	require.Len(t, out.Results, 1)
	require.Equal(t, extensionApplyKind, out.Results[0].Kind)
	require.Equal(t, arv0.ApplyStatusCreated, out.Results[0].Status)

	row, err := extensionStore.Get(t.Context(), v1alpha1.DefaultNamespace, "enterprise-only", "1")
	require.NoError(t, err)
	require.JSONEq(t, `{"value":"ok"}`, string(row.Spec))
}
