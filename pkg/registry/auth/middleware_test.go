package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
)

// testAuthn succeeds authenticating only requests carrying "Bearer ok", attaching the testSession session.
type testAuthn struct{}

func (testAuthn) Authenticate(_ context.Context, headers func(name string) string, _ url.Values) (auth.Session, error) {
	if headers("Authorization") == "Bearer ok" {
		return &testSession{}, nil
	}
	return nil, errors.New("missing or invalid credentials")
}

type testSession struct{}

func (s *testSession) Principal() auth.Principal { return auth.Principal{} }

// sessionCapture records what the handler saw for the request: whether it ran
// at all (401 short-circuits before handler), and the session the middleware attached.
type sessionCapture struct {
	handled bool
	session auth.Session
}

func (c *sessionCapture) reset() {
	c.handled = false
	c.session = nil
}

// newTestAPI builds a humatest API with optional middleware installed.
func newTestAPI(t *testing.T, opts ...auth.MiddlewareOption) (humatest.TestAPI, *sessionCapture) {
	t.Helper()
	capture := &sessionCapture{}
	record := func(ctx context.Context) {
		capture.handled = true
		capture.session, _ = auth.AuthSessionFrom(ctx)
	}

	_, api := humatest.New(t)
	api.UseMiddleware(auth.AuthnMiddleware(testAuthn{}, opts...))

	get := func(operationID, path string) huma.Operation {
		return huma.Operation{OperationID: operationID, Method: http.MethodGet, Path: path}
	}
	// okOutput carries a body so huma encodes handled requests as 200, instead of 204 (no body)
	type okOutput struct {
		Body struct {
			OK bool `json:"ok"`
		}
	}
	handled := func(ctx context.Context) (*okOutput, error) {
		record(ctx)
		out := &okOutput{}
		out.Body.OK = true
		return out, nil
	}
	h := func(ctx context.Context, _ *struct{}) (*okOutput, error) {
		return handled(ctx)
	}

	huma.Register(api, get("health", "/health"), h)
	huma.Register(api, get("list-servers", "/v0.1/servers"), h)
	huma.Register(api, get("private-list", "/v0/mcpservers"), h)
	huma.Register(api, get("sibling-list", "/v0.1sibling/servers"), h)
	huma.Register(api, get("list-versions", "/v0.1/servers/{serverName}/versions"), h)
	huma.Register(api, get("get-version", "/v0.1/servers/{serverName}/versions/{version}"), h)

	return api, capture
}

func TestAuthnMiddlewarePublicPaths(t *testing.T) {
	api, capture := newTestAPI(t,
		auth.WithSkipPaths("/health"),
		auth.WithPublicPaths("/v0.1/"),
	)

	tests := []struct {
		name        string
		path        string
		token       string
		wantStatus  int
		wantSession auth.Session
	}{
		{
			name:        "public list without credentials",
			path:        "/v0.1/servers",
			wantStatus:  http.StatusOK,
			wantSession: &auth.PublicSession{},
		},
		{
			name:        "public parameterized versions list without credentials",
			path:        "/v0.1/servers/default%2Fgithub-mcp/versions",
			wantStatus:  http.StatusOK,
			wantSession: &auth.PublicSession{},
		},
		{
			name:        "public parameterized version get without credentials",
			path:        "/v0.1/servers/default%2Fgithub-mcp/versions/latest",
			wantStatus:  http.StatusOK,
			wantSession: &auth.PublicSession{},
		},
		{
			name:        "public path ignores presented credentials",
			path:        "/v0.1/servers",
			token:       "Bearer ok",
			wantStatus:  http.StatusOK,
			wantSession: &auth.PublicSession{},
		},
		{
			name:        "public path ignores invalid credentials",
			path:        "/v0.1/servers",
			token:       "Bearer expired",
			wantStatus:  http.StatusOK,
			wantSession: &auth.PublicSession{},
		},
		{
			name:       "private path without credentials is rejected",
			path:       "/v0/mcpservers",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:        "private path with credentials carries the configured authn session",
			path:        "/v0/mcpservers",
			token:       "Bearer ok",
			wantStatus:  http.StatusOK,
			wantSession: &testSession{},
		},
		{
			name:        "skip path carries no session",
			path:        "/health",
			wantStatus:  http.StatusOK,
			wantSession: nil,
		},
		{
			name:       "sibling path sharing the prefix text is not public",
			path:       "/v0.1sibling/servers",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture.reset()
			var resp *httptest.ResponseRecorder
			if tt.token != "" {
				resp = api.Get(tt.path, "Authorization: "+tt.token)
			} else {
				resp = api.Get(tt.path)
			}

			assert.Equal(t, tt.wantStatus, resp.Code)
			if tt.wantStatus != http.StatusOK {
				assert.False(t, capture.handled, "handler must not run on rejected requests")
				return
			}
			assert.True(t, capture.handled)
			assert.Equal(t, tt.wantSession, capture.session)
		})
	}
}

func TestAuthnMiddlewareWithoutPublicPaths(t *testing.T) {
	api, capture := newTestAPI(t, auth.WithSkipPaths("/health"))

	resp := api.Get("/v0.1/servers")

	assert.Equal(t, http.StatusUnauthorized, resp.Code)
	assert.False(t, capture.handled)
}
