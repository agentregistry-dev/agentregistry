package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"

	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
	arv0 "github.com/agentregistry-dev/agentregistry/pkg/api/v0"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

type rejectingAuthnProvider struct {
	calls int
}

func (p *rejectingAuthnProvider) Authenticate(context.Context, func(string) string, url.Values) (auth.Session, error) {
	p.calls++
	return nil, auth.ErrUnauthenticated
}

func TestNewHumaAPIAuthenticationBoundaries(t *testing.T) {
	metrics, err := telemetry.NewMetrics(otel.GetMeterProvider().Meter("router-test"))
	require.NoError(t, err)

	mux := http.NewServeMux()
	provider := &rejectingAuthnProvider{}
	_, err = NewHumaAPI(
		&config.Config{PluginMarketplaceCompatEnabled: true},
		mux,
		metrics,
		&arv0.VersionBody{},
		nil,
		provider,
		&RouteOptions{Stores: Stores{v1alpha1.KindPlugin: &v1alpha1store.Store{}}},
		nil,
	)
	require.NoError(t, err)

	t.Run("marketplace uses configured authentication", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/plugin-marketplace/marketplace.json", nil))

		require.Equal(t, http.StatusUnauthorized, recorder.Code)
		require.Equal(t, 1, provider.calls)
	})

	t.Run("skip route remains unauthenticated", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v0/ping", nil))

		require.Equal(t, http.StatusOK, recorder.Code)
		require.Equal(t, 1, provider.calls)
	})
}
