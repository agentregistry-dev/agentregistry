package ping_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/assert"

	v0ping "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/ping"
)

func TestPingEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

	v0ping.RegisterPingEndpoint(api, "/v0")

	req := httptest.NewRequest(http.MethodGet, "/v0/ping", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"pong":true`)
}
