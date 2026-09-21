package resource_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
)

func TestResourceRegister_DeleteMutableDeniedBeforeStoreRead(t *testing.T) {
	var seen []resource.AuthorizeInput
	_, api := humatest.New(t)
	resource.Register[*v1alpha1.Runtime](api, resource.Config{
		Kind:       v1alpha1.KindRuntime,
		BasePrefix: "/v0",
		// A nil store makes any pre-authorization lookup fail. No delete
		// callbacks are configured, so the stored object is not needed.
		Store: nil,
		Authorize: func(_ context.Context, in resource.AuthorizeInput) error {
			seen = append(seen, in)
			return huma.Error403Forbidden("delete denied")
		},
	}, func() *v1alpha1.Runtime { return &v1alpha1.Runtime{} })

	var resp *httptest.ResponseRecorder
	require.NotPanics(t, func() {
		resp = api.Delete("/v0/runtimes/example.com%2Fbroken?namespace=delete-auth")
	}, "denied DELETE must not read the store")
	require.Equal(t, http.StatusForbidden, resp.Code, resp.Body.String())
	require.Equal(t, []resource.AuthorizeInput{{
		Verb: "delete", Kind: v1alpha1.KindRuntime,
		Namespace: "delete-auth", Name: "example.com/broken",
	}}, seen, "authorize exactly once, with path identity and no unused object")
}
