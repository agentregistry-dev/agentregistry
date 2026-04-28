//go:build integration

package deploymentlogs_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/deploymentlogs"
	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/platforms/noop"
	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// TestRegisterDeploymentLogs_RespectsAuthorize pins the row-level
// RBAC invariant for the logs subresource. Without this gate, any
// authenticated caller can drain runtime stdout/stderr (frequently
// containing PII, secrets, or DB queries) for any deployment
// regardless of role grants — much worse than the README leak C3a
// covers because the surface streams runtime output rather than just
// declared spec content.
func TestRegisterDeploymentLogs_RespectsAuthorize(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	stores := v1alpha1store.NewStores(pool)
	deployments := stores[v1alpha1.KindDeployment]

	coord := deploymentsvc.NewCoordinator(deploymentsvc.Dependencies{
		Stores:   stores,
		Adapters: map[string]types.DeploymentAdapter{noop.Platform: noop.New()},
		Getter:   internaldb.NewGetter(stores),
	})

	authorize := func(ctx context.Context, in resource.AuthorizeInput) error {
		if in.Name == "secret" {
			return huma.Error403Forbidden("denied")
		}
		return nil
	}

	_, api := humatest.New(t)
	deploymentlogs.Register(api, deploymentlogs.Config{
		BasePrefix:  "/v0",
		Store:       deployments,
		Coordinator: coord,
		Authorize:   authorize,
	})

	// Denied row → 403. The gate fires before Store.Get, so the row
	// doesn't even need to exist for the deny path to be testable —
	// the existence-leak via 404-vs-403 is exactly the kind of
	// information disclosure this gate prevents.
	resp := api.Get("/v0/deployments/secret/v1/logs")
	require.Equal(t, http.StatusForbidden, resp.Code, resp.Body.String())

	// Allowed name without a seeded row → 404. Confirms the gate is
	// not a deny-everything bug — non-denied requests reach the
	// Store.Get and get the regular not-found response.
	resp = api.Get("/v0/deployments/nonexistent/v1/logs")
	require.Equal(t, http.StatusNotFound, resp.Code, resp.Body.String())
}

// TestRegisterDeploymentLogs_NilAuthorizeAllowsThrough confirms the
// nil-authz path matches the OSS default ("public reads with
// router-level auth"). Mostly a guard against a refactor that
// silently flips the default to deny.
func TestRegisterDeploymentLogs_NilAuthorizeAllowsThrough(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	stores := v1alpha1store.NewStores(pool)
	deployments := stores[v1alpha1.KindDeployment]

	coord := deploymentsvc.NewCoordinator(deploymentsvc.Dependencies{
		Stores:   stores,
		Adapters: map[string]types.DeploymentAdapter{noop.Platform: noop.New()},
		Getter:   internaldb.NewGetter(stores),
	})

	_, api := humatest.New(t)
	deploymentlogs.Register(api, deploymentlogs.Config{
		BasePrefix:  "/v0",
		Store:       deployments,
		Coordinator: coord,
		Authorize:   nil,
	})

	resp := api.Get("/v0/deployments/anything/v1/logs")
	require.Equal(t, http.StatusNotFound, resp.Code,
		"nil Authorize must not 403 — must fall through to Store.Get and 404")
}
