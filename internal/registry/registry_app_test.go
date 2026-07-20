package registry

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

func TestDeploymentControllerConfigMapsRetentionSettings(t *testing.T) {
	cfg := &config.Config{
		ControllerEventRetention:             2 * time.Hour,
		ControllerEventKeepAfterRevision:     42,
		ControllerRetentionPruneBatchLimit:   17,
		ControllerDiscoveryInterval:          15 * time.Second,
		ControllerDiscoveryStaleAfterMisses:  2,
		ControllerDiscoveryDeleteAfterMisses: 4,
	}

	got := deploymentControllerConfig(cfg)

	require.Equal(t, 2*time.Hour, got.Retention.ControlPlaneEvents)
	require.Equal(t, int64(42), got.Retention.EventKeepAfterRev)
	require.Equal(t, 17, got.Retention.BatchLimit)
	require.Equal(t, 15*time.Second, got.DiscoveryInterval)
	require.Equal(t, 2, got.DiscoveryStaleAfterMisses)
	require.Equal(t, 4, got.DiscoveryDeleteAfterMisses)
}

func TestBuildStoresAddsExtraStoreTables(t *testing.T) {
	stores := buildStores(nil, map[string]string{
		"ExtensionOnly": "extension_only",
	}, nil, nil)
	if stores["ExtensionOnly"] == nil {
		t.Fatalf("extra v1alpha1 store was not registered")
	}
}

func TestResolveExtraStoreSchema(t *testing.T) {
	oss := pkgdb.MustNewSchema(pkgdb.OSSSchema)
	tests := []struct {
		name       string
		table      string
		wantSchema string
		wantTable  string
	}{
		{"bare table stays in OSS schema", "widgets", pkgdb.OSSSchema, "widgets"},
		{"qualified resolves to its schema", "ext.widgets", "ext", "widgets"},
		{"splits on first dot only", "ext.a.b", "ext", "a.b"},
		{"trailing dot yields empty table", "ext.", "ext", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSchema, gotTable := resolveExtraStoreSchema(tt.table, oss)
			if gotSchema.Name() != tt.wantSchema {
				t.Errorf("schema = %q, want %q", gotSchema.Name(), tt.wantSchema)
			}
			if gotTable != tt.wantTable {
				t.Errorf("table = %q, want %q", gotTable, tt.wantTable)
			}
		})
	}
}

func TestResolveExtraStoreSchemaPanicsOnInvalidSchema(t *testing.T) {
	oss := pkgdb.MustNewSchema(pkgdb.OSSSchema)
	defer func() {
		if recover() == nil {
			t.Fatalf("expected panic on invalid schema identifier")
		}
	}()
	resolveExtraStoreSchema("BadSchema.widgets", oss)
}

// fakeSession is a minimal auth.Session for exercising the bridge middleware.
type fakeSession struct{}

func (fakeSession) Principal() auth.Principal { return auth.Principal{} }

// fakeAuthnProvider returns a fixed (session, err) so tests can drive the
// middleware's accept/reject branches without a real token or IdP.
type fakeAuthnProvider struct {
	session auth.Session
	err     error
}

func (f fakeAuthnProvider) Authenticate(context.Context, func(string) string, url.Values) (auth.Session, error) {
	return f.session, f.err
}

func TestMCPAuthnMiddleware(t *testing.T) {
	const metaURL = "https://host.example/.well-known/oauth-protected-resource/mcp"

	newNext := func(ran, sawSession *bool) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*ran = true
			_, ok := auth.AuthSessionFrom(r.Context())
			*sawSession = ok
			w.WriteHeader(http.StatusTeapot)
		})
	}

	t.Run("valid token passes through and attaches the session", func(t *testing.T) {
		var ran, sawSession bool
		h := mcpAuthnMiddleware(fakeAuthnProvider{session: fakeSession{}}, metaURL)(newNext(&ran, &sawSession))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
		assert.Equal(t, http.StatusTeapot, rec.Code)
		assert.True(t, ran, "next handler should run")
		assert.True(t, sawSession, "authenticated session must be on the request context")
		assert.Empty(t, rec.Header().Get("WWW-Authenticate"))
	})

	t.Run("authentication error is rejected with 401", func(t *testing.T) {
		var ran, sawSession bool
		h := mcpAuthnMiddleware(fakeAuthnProvider{err: errors.New("bad token")}, metaURL)(newNext(&ran, &sawSession))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.False(t, ran, "next handler must not run when authn fails")
	})

	t.Run("nil session without error is rejected with 401", func(t *testing.T) {
		var ran, sawSession bool
		h := mcpAuthnMiddleware(fakeAuthnProvider{}, metaURL)(newNext(&ran, &sawSession))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.False(t, ran)
	})

	t.Run("401 carries the MCP server challenge when a metadata url is set", func(t *testing.T) {
		var ran, sawSession bool
		h := mcpAuthnMiddleware(fakeAuthnProvider{err: errors.New("unauthenticated")}, metaURL)(newNext(&ran, &sawSession))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.Equal(t, `Bearer resource_metadata="`+metaURL+`"`, rec.Header().Get("WWW-Authenticate"))
	})

	t.Run("401 omits the MCP server challenge when no metadata url is set", func(t *testing.T) {
		var ran, sawSession bool
		h := mcpAuthnMiddleware(fakeAuthnProvider{err: errors.New("no")}, "")(newNext(&ran, &sawSession))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.Empty(t, rec.Header().Get("WWW-Authenticate"))
	})
}

func TestBuildMCPMux(t *testing.T) {
	// catchAll marks a request that fell through to the MCP handler.
	const catchAll = http.StatusTeapot
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(catchAll) })
	meta := &oauthex.ProtectedResourceMetadata{
		Resource:             "https://host.example/mcp",
		AuthorizationServers: []string{"https://issuer.example"},
	}

	t.Run("/healthz returns 200", func(t *testing.T) {
		rec := httptest.NewRecorder()
		buildMCPMux(handler, meta).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("serves protected-resource metadata (exact and trailing-slash) when supplied", func(t *testing.T) {
		mux := buildMCPMux(handler, meta)
		for _, path := range []string{
			"/.well-known/oauth-protected-resource",
			"/.well-known/oauth-protected-resource/",
		} {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			require.Equal(t, http.StatusOK, rec.Code, path)
			var got oauthex.ProtectedResourceMetadata
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got), path)
			assert.Equal(t, meta.Resource, got.Resource, path)
			assert.Equal(t, meta.AuthorizationServers, got.AuthorizationServers, path)
		}
	})

	t.Run("no metadata route when metadata is nil (falls through to the MCP handler)", func(t *testing.T) {
		rec := httptest.NewRecorder()
		buildMCPMux(handler, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
		assert.Equal(t, catchAll, rec.Code, "nil metadata must not mount a discovery route")
	})

	t.Run("catch-all routes to the MCP handler", func(t *testing.T) {
		rec := httptest.NewRecorder()
		buildMCPMux(handler, meta).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
		assert.Equal(t, catchAll, rec.Code)
	})
}
