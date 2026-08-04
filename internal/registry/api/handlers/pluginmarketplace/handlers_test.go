package pluginmarketplace_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	handler "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/pluginmarketplace"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/pluginmarketplace"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// fakeStore is an in-memory PluginStore for handler tests. It paginates its
// rows one at a time so tests exercise the handler's cursor-following loop,
// and records every ListOpts it was called with.
type fakeStore struct {
	rows     []*v1alpha1.RawObject
	listErr  error
	lastOpts []v1alpha1store.ListOpts
}

func (f *fakeStore) List(_ context.Context, opts v1alpha1store.ListOpts) ([]*v1alpha1.RawObject, string, error) {
	f.lastOpts = append(f.lastOpts, opts)
	if f.listErr != nil {
		return nil, "", f.listErr
	}
	start := 0
	if opts.Cursor != "" {
		var err error
		start, err = strconv.Atoi(opts.Cursor)
		if err != nil {
			return nil, "", v1alpha1store.ErrInvalidCursor
		}
	}
	if start >= len(f.rows) {
		return nil, "", nil
	}
	end := start + 1
	next := ""
	if end < len(f.rows) {
		next = strconv.Itoa(end)
	}
	return f.rows[start:end], next, nil
}

func rawPlugin(t *testing.T, namespace, name string, spec v1alpha1.PluginSpec, status v1alpha1.PluginStatus) *v1alpha1.RawObject {
	t.Helper()
	specJSON, err := json.Marshal(spec)
	require.NoError(t, err)
	statusJSON, err := json.Marshal(status)
	require.NoError(t, err)
	return &v1alpha1.RawObject{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindPlugin},
		Metadata: v1alpha1.ObjectMeta{Namespace: namespace, Name: name, Tag: "latest"},
		Spec:     specJSON,
		Status:   statusJSON,
	}
}

func readyCondition() v1alpha1.Status {
	var s v1alpha1.Status
	s.SetCondition(v1alpha1.Condition{Type: "Ready", Status: v1alpha1.ConditionTrue, Reason: "Resolved"})
	return s
}

func notReadyCondition() v1alpha1.Status {
	var s v1alpha1.Status
	s.SetCondition(v1alpha1.Condition{Type: "Ready", Status: v1alpha1.ConditionFalse, Reason: "Progressing"})
	return s
}

func newAPI(t *testing.T, cfg handler.Config) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	handler.Register(api, cfg)
	return mux
}

func doGet(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestGetMarketplace_TranslatesReadyPlugins(t *testing.T) {
	ready := rawPlugin(t, "default", "code-formatter",
		v1alpha1.PluginSpec{
			Description: "Formats code on save",
			Source: &v1alpha1.PluginSource{
				Type: v1alpha1.PluginSourceTypeGit,
				Git: &v1alpha1.PluginSourceGit{
					Repository: &v1alpha1.Repository{URL: "https://github.com/acme/code-formatter"},
				},
			},
		},
		v1alpha1.PluginStatus{
			Status:         readyCondition(),
			ResolvedSource: &v1alpha1.PluginResolvedSource{Type: v1alpha1.PluginSourceTypeGit, Commit: "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"},
			Manifest:       &v1alpha1.PluginManifest{Name: "code-formatter", Version: "1.2.0", Description: "Formats code on save"},
		})

	notReady := rawPlugin(t, "default", "still-resolving",
		v1alpha1.PluginSpec{
			Source: &v1alpha1.PluginSource{
				Type: v1alpha1.PluginSourceTypeGit,
				Git: &v1alpha1.PluginSourceGit{
					Repository: &v1alpha1.Repository{URL: "https://github.com/acme/still-resolving"},
				},
			},
		},
		v1alpha1.PluginStatus{Status: notReadyCondition()})

	ociPlugin := rawPlugin(t, "default", "oci-plugin",
		v1alpha1.PluginSpec{
			Source: &v1alpha1.PluginSource{
				Type: v1alpha1.PluginSourceTypeOCI,
				OCI:  &v1alpha1.PluginSourceOCI{Reference: "ghcr.io/acme/oci-plugin@sha256:deadbeef"},
			},
		},
		v1alpha1.PluginStatus{
			Status:         readyCondition(),
			ResolvedSource: &v1alpha1.PluginResolvedSource{Type: v1alpha1.PluginSourceTypeOCI, Digest: "sha256:deadbeef"},
		})

	store := &fakeStore{rows: []*v1alpha1.RawObject{ready, notReady, ociPlugin}}
	h := newAPI(t, handler.Config{Store: store})

	rec := doGet(t, h, "/plugin-marketplace/marketplace.json")
	require.Equal(t, http.StatusOK, rec.Code)

	var got pluginmarketplace.MarketplaceResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))

	want := pluginmarketplace.MarketplaceResponse{
		Schema: pluginmarketplace.SchemaURL,
		Name:   handler.DefaultMarketplaceName,
		Owner:  pluginmarketplace.Owner{Name: handler.DefaultMarketplaceName},
		Plugins: []pluginmarketplace.PluginEntry{
			{
				Name: "code-formatter",
				Source: map[string]any{
					"source": "url",
					"url":    "https://github.com/acme/code-formatter",
					"sha":    "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
				},
				Description: "Formats code on save",
				Version:     "1.2.0",
			},
		},
	}
	assert.Equal(t, want, got)

	// Confirms the handler followed the fake's cursor across all three rows
	// rather than stopping after the first page.
	assert.Len(t, store.lastOpts, 3)
}

func TestGetMarketplace_EmptyCatalogueEmitsEmptyArray(t *testing.T) {
	// Claude Code's marketplace.json parser rejects a null plugins field, so
	// an empty catalogue must marshal as "plugins":[] rather than
	// "plugins":null. json.Unmarshal into pluginmarketplace.MarketplaceResponse
	// can't distinguish the two (both decode to a nil slice), so this decodes
	// Plugins as json.RawMessage to inspect the actual encoded value.
	store := &fakeStore{rows: nil}
	h := newAPI(t, handler.Config{Store: store})

	rec := doGet(t, h, "/plugin-marketplace/marketplace.json")
	require.Equal(t, http.StatusOK, rec.Code)

	var got struct {
		Plugins json.RawMessage `json:"plugins"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.JSONEq(t, "[]", string(got.Plugins))
}

func TestGetMarketplace_AppliesListFilter(t *testing.T) {
	store := &fakeStore{rows: nil}
	var gotAuthorizeInput resource.AuthorizeInput
	cfg := handler.Config{
		Store: store,
		ListFilter: func(_ context.Context, in resource.AuthorizeInput) (string, []any, error) {
			gotAuthorizeInput = in
			return "namespace = $1", []any{"team-a"}, nil
		},
	}
	h := newAPI(t, cfg)

	rec := doGet(t, h, "/plugin-marketplace/marketplace.json")
	require.Equal(t, http.StatusOK, rec.Code)

	require.Len(t, store.lastOpts, 1)
	assert.Equal(t, "(namespace = $1)", store.lastOpts[0].ExtraWhere)
	assert.Equal(t, []any{"team-a"}, store.lastOpts[0].ExtraArgs)
	assert.Equal(t, resource.AuthorizeInput{Verb: "list", Kind: v1alpha1.KindPlugin}, gotAuthorizeInput)
}

func TestGetMarketplace_ListFilterError(t *testing.T) {
	store := &fakeStore{}
	cfg := handler.Config{
		Store: store,
		ListFilter: func(context.Context, resource.AuthorizeInput) (string, []any, error) {
			return "", nil, errors.New("boom")
		},
	}
	h := newAPI(t, cfg)

	rec := doGet(t, h, "/plugin-marketplace/marketplace.json")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestGetMarketplace_StoreError(t *testing.T) {
	store := &fakeStore{listErr: errors.New("db down")}
	h := newAPI(t, handler.Config{Store: store})

	rec := doGet(t, h, "/plugin-marketplace/marketplace.json")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
