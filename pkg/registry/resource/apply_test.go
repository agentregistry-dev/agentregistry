//go:build integration

package resource_test

import (
	"context"
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

func TestRegisterApply_MultiDocRoundTrip(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	agents := v1alpha1store.NewStore(pool, "v1alpha1.agents")
	mcps := v1alpha1store.NewStore(pool, "v1alpha1.mcp_servers")

	_, api := humatest.New(t)
	resource.RegisterApply(api, resource.ApplyConfig{
		BasePrefix: "/v0",
		Stores: map[string]*v1alpha1store.Store{
			v1alpha1.KindAgent:     agents,
			v1alpha1.KindMCPServer: mcps,
		},
	})

	yaml := []byte(`apiVersion: ar.dev/v1alpha1
kind: MCPServer
metadata:
  namespace: default
  name: tools
  version: v1
spec:
  title: Tools
---
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  namespace: default
  name: alice
  version: v1
spec:
  title: Alice
  mcpServers:
    - kind: MCPServer
      name: tools
      version: v1
`)
	resp := api.Post("/v0/apply", "Content-Type: application/yaml", strings.NewReader(string(yaml)))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var out struct {
		Results []arv0.ApplyResult `json:"results"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	require.Len(t, out.Results, 2)
	require.Equal(t, v1alpha1.KindMCPServer, out.Results[0].Kind)
	require.Equal(t, arv0.ApplyStatusCreated, out.Results[0].Status)
	require.Equal(t, v1alpha1.KindAgent, out.Results[1].Kind)
	require.Equal(t, arv0.ApplyStatusCreated, out.Results[1].Status)

	// Re-apply identical YAML: both should report "unchanged".
	resp = api.Post("/v0/apply", "Content-Type: application/yaml", strings.NewReader(string(yaml)))
	require.Equal(t, http.StatusOK, resp.Code)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	require.Equal(t, arv0.ApplyStatusUnchanged, out.Results[0].Status)
	require.Equal(t, arv0.ApplyStatusUnchanged, out.Results[1].Status)
}

func TestRegisterApply_PerDocFailureDoesntAbortBatch(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	agents := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	_, api := humatest.New(t)
	resource.RegisterApply(api, resource.ApplyConfig{
		BasePrefix: "/v0",
		Stores: map[string]*v1alpha1store.Store{
			v1alpha1.KindAgent: agents,
		},
	})

	// Two docs: first valid, second references a non-configured kind.
	yaml := []byte(`apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  namespace: default
  name: good
  version: v1
spec:
  title: Good
---
apiVersion: ar.dev/v1alpha1
kind: Skill
metadata:
  namespace: default
  name: nope
  version: v1
spec:
  title: Nope
`)
	resp := api.Post("/v0/apply", "Content-Type: application/yaml", strings.NewReader(string(yaml)))
	require.Equal(t, http.StatusOK, resp.Code)

	var out struct {
		Results []arv0.ApplyResult `json:"results"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	require.Len(t, out.Results, 2)
	require.Equal(t, arv0.ApplyStatusCreated, out.Results[0].Status)
	require.Equal(t, arv0.ApplyStatusFailed, out.Results[1].Status)
	require.Contains(t, out.Results[1].Error, "unknown or unconfigured kind")
}

func TestRegisterApply_ValidationFailsPerDoc(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	agents := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	_, api := humatest.New(t)
	resource.RegisterApply(api, resource.ApplyConfig{
		BasePrefix: "/v0",
		Stores:     map[string]*v1alpha1store.Store{v1alpha1.KindAgent: agents},
	})

	yaml := []byte(`apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  namespace: default
  name: bad
  version: latest
spec:
  title: Bad
`)
	resp := api.Post("/v0/apply", "Content-Type: application/yaml", strings.NewReader(string(yaml)))
	require.Equal(t, http.StatusOK, resp.Code)

	var out struct {
		Results []arv0.ApplyResult `json:"results"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	require.Len(t, out.Results, 1)
	require.Equal(t, arv0.ApplyStatusFailed, out.Results[0].Status)
	require.Contains(t, out.Results[0].Error, "metadata.version")
}

// TestRegisterApply_DeniesKindWithNoAuthorizer pins the apply-side
// fail-closed contract: when ApplyConfig.Authorizers is non-empty
// (i.e. authz is wired) but the doc's kind has no entry, the doc
// fails with "no authorizer wired" rather than silently authorizing.
//
// Mirrors the import-handler N1 fix in `f8682fb`. Without this, an
// operator who misconfigures PerKindHooks — wires an authorizer for
// some kinds but forgets others — would silently bypass authz on the
// /v0/apply path for the missing kinds.
func TestRegisterApply_DeniesKindWithNoAuthorizer(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	agents := v1alpha1store.NewStore(pool, "v1alpha1.agents")
	mcps := v1alpha1store.NewStore(pool, "v1alpha1.mcp_servers")

	_, api := humatest.New(t)
	resource.RegisterApply(api, resource.ApplyConfig{
		BasePrefix: "/v0",
		Stores: map[string]*v1alpha1store.Store{
			v1alpha1.KindAgent:     agents,
			v1alpha1.KindMCPServer: mcps,
		},
		// Authorizers wired for Agent only; MCPServer intentionally absent.
		Authorizers: map[string]func(context.Context, resource.AuthorizeInput) error{
			v1alpha1.KindAgent: func(_ context.Context, _ resource.AuthorizeInput) error { return nil },
		},
	})

	yaml := []byte(`apiVersion: ar.dev/v1alpha1
kind: MCPServer
metadata:
  namespace: default
  name: should-be-denied
  version: v1
spec:
  title: Should be denied
`)
	resp := api.Post("/v0/apply", "Content-Type: application/yaml", strings.NewReader(string(yaml)))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var out struct {
		Results []arv0.ApplyResult `json:"results"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	require.Len(t, out.Results, 1)
	require.Equal(t, arv0.ApplyStatusFailed, out.Results[0].Status)
	require.Contains(t, out.Results[0].Error, `no authorizer wired for kind "MCPServer"`)

	// And the row didn't land in the store.
	_, err := mcps.Get(t.Context(), "default", "should-be-denied", "v1")
	require.Error(t, err, "fail-closed must short-circuit before Upsert")
}
