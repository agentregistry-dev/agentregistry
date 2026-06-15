package mcpregistry_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	handler "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/mcpregistry"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/mcpregistry"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// fakeStore is an in-memory ServerStore for handler tests. It records the last
// ListOpts so tests can assert how query params were translated.
type fakeStore struct {
	rows       []*v1alpha1.RawObject
	nextCursor string
	listErr    error
	lastOpts   v1alpha1store.ListOpts
}

func (f *fakeStore) List(_ context.Context, opts v1alpha1store.ListOpts) ([]*v1alpha1.RawObject, string, error) {
	f.lastOpts = opts
	if f.listErr != nil {
		return nil, "", f.listErr
	}
	return f.rows, f.nextCursor, nil
}

func (f *fakeStore) GetLatest(_ context.Context, namespace, name string) (*v1alpha1.RawObject, error) {
	for _, r := range f.rows {
		if r.Metadata.NamespaceOrDefault() == namespace && r.Metadata.Name == name {
			return r, nil
		}
	}
	return nil, pkgdb.ErrNotFound
}

func (f *fakeStore) Get(_ context.Context, namespace, name, tag string) (*v1alpha1.RawObject, error) {
	for _, r := range f.rows {
		if r.Metadata.NamespaceOrDefault() == namespace && r.Metadata.Name == name && r.Metadata.Tag == tag {
			return r, nil
		}
	}
	return nil, pkgdb.ErrNotFound
}

func rawMCPServer(t *testing.T, namespace, name, tag string, spec v1alpha1.MCPServerSpec) *v1alpha1.RawObject {
	t.Helper()
	specJSON, err := json.Marshal(spec)
	require.NoError(t, err)
	return &v1alpha1.RawObject{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindMCPServer},
		Metadata: v1alpha1.ObjectMeta{Namespace: namespace, Name: name, Tag: tag},
		Spec:     specJSON,
	}
}

func newAPI(t *testing.T, store handler.ServerStore) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	handler.Register(api, "", store)
	return mux
}

func npmSpec(title string) v1alpha1.MCPServerSpec {
	return v1alpha1.MCPServerSpec{
		Title:       title,
		Description: title,
		Source: &v1alpha1.MCPServerSource{
			Package: &v1alpha1.MCPPackage{
				Origin: v1alpha1.MCPPackageOrigin{
					Type:       v1alpha1.MCPPackageOriginTypeNPM,
					Identifier: "weather-mcp",
					NPM:        &v1alpha1.MCPPackageOriginNPM{Version: "1.0.0", ServerName: "io.github.acme/weather"},
				},
				Transport: v1alpha1.MCPTransport{Type: "stdio"},
			},
		},
	}
}

func TestListServers_Envelope(t *testing.T) {
	store := &fakeStore{
		rows: []*v1alpha1.RawObject{
			rawMCPServer(t, "team-a", "weather", "latest", npmSpec("Weather")),
		},
		nextCursor: "CURSOR123",
	}
	srv := newAPI(t, store)

	req := httptest.NewRequest(http.MethodGet, "/v0.1/servers", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp mcpregistry.ServerListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Servers, 1)
	assert.Equal(t, "team-a/weather", resp.Servers[0].Server.Name)
	require.NotNil(t, resp.Servers[0].Meta)
	require.NotNil(t, resp.Servers[0].Meta.Official)
	assert.True(t, resp.Servers[0].Meta.Official.IsLatest)
	assert.Equal(t, 1, resp.Metadata.Count)
	assert.Equal(t, "CURSOR123", resp.Metadata.NextCursor)

	// Default list flattens all namespaces and serves the latest tag only.
	assert.Empty(t, store.lastOpts.Namespace)
	assert.True(t, store.lastOpts.LatestOnly)
	assert.Empty(t, store.lastOpts.ExtraWhere)
}

func TestListServers_VersionAndFilters(t *testing.T) {
	store := &fakeStore{}
	srv := newAPI(t, store)

	req := httptest.NewRequest(http.MethodGet,
		"/v0.1/servers?version=2.0.0&search=weather&updated_since=2026-04-01T00:00:00Z", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	// version=specific pins the tag instead of latest-only.
	assert.False(t, store.lastOpts.LatestOnly)
	assert.Equal(t, "2.0.0", store.lastOpts.Tag)
	// search + updated_since build a two-predicate ExtraWhere with matching args.
	assert.Equal(t, "name ILIKE $1 AND updated_at >= $2", store.lastOpts.ExtraWhere)
	require.Len(t, store.lastOpts.ExtraArgs, 2)
	assert.Equal(t, "%weather%", store.lastOpts.ExtraArgs[0])
}

func TestListServers_BadUpdatedSince(t *testing.T) {
	srv := newAPI(t, &fakeStore{})
	req := httptest.NewRequest(http.MethodGet, "/v0.1/servers?updated_since=not-a-time", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetServerVersion_LatestAndEncodedName(t *testing.T) {
	store := &fakeStore{
		rows: []*v1alpha1.RawObject{
			rawMCPServer(t, "team-a", "weather", "latest", npmSpec("Weather")),
		},
	}
	srv := newAPI(t, store)

	// "team-a/weather" arrives URL-encoded as a single path segment.
	req := httptest.NewRequest(http.MethodGet, "/v0.1/servers/team-a%2Fweather/versions/latest", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp mcpregistry.ServerResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "team-a/weather", resp.Server.Name)
	assert.Equal(t, "1.0.0", resp.Server.Version)
}

func TestGetServerVersion_NotFound(t *testing.T) {
	srv := newAPI(t, &fakeStore{})
	req := httptest.NewRequest(http.MethodGet, "/v0.1/servers/team-a%2Fmissing/versions/latest", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetServerVersion_MalformedName(t *testing.T) {
	srv := newAPI(t, &fakeStore{})
	// A name with no slash can't be split into namespace/name.
	req := httptest.NewRequest(http.MethodGet, "/v0.1/servers/noslash/versions/latest", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestListServerVersions(t *testing.T) {
	store := &fakeStore{
		rows: []*v1alpha1.RawObject{
			rawMCPServer(t, "team-a", "weather", "latest", npmSpec("Weather")),
		},
	}
	srv := newAPI(t, store)

	req := httptest.NewRequest(http.MethodGet, "/v0.1/servers/team-a%2Fweather/versions", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp mcpregistry.ServerListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Servers, 1)
	// Scoped to the parsed namespace + name.
	assert.Equal(t, "team-a", store.lastOpts.Namespace)
	assert.Equal(t, "name = $1", store.lastOpts.ExtraWhere)
	require.Len(t, store.lastOpts.ExtraArgs, 1)
	assert.Equal(t, "weather", store.lastOpts.ExtraArgs[0])
}

func TestListServerVersions_NotFound(t *testing.T) {
	srv := newAPI(t, &fakeStore{})
	req := httptest.NewRequest(http.MethodGet, "/v0.1/servers/team-a%2Fmissing/versions", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
