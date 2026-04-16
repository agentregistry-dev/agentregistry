package apply_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/apply"
	"github.com/agentregistry-dev/agentregistry/internal/registry/kinds"
)

// demoSpec is the spec type for the synthetic "demo" kind used in tests.
type demoSpec struct {
	Message string `yaml:"message" json:"message"`
}

// applyResponse mirrors the response body shape.
type applyResponse struct {
	Results []kinds.Result `json:"results"`
}

// setupTestAPI registers the apply endpoints with a registry containing the
// provided kind (if non-zero) and returns the HTTP handler.
func setupTestAPI(t *testing.T, kindsToRegister ...kinds.Kind) (*http.ServeMux, *kinds.Registry) {
	t.Helper()
	reg := kinds.NewRegistry()
	for _, k := range kindsToRegister {
		reg.Register(k)
	}
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("test", "1.0.0"))
	apply.RegisterApplyEndpoints(api, "/v0", reg)
	return mux, reg
}

func demoKind(applyFn kinds.ApplyFunc, deleteFn kinds.DeleteFunc) kinds.Kind {
	return kinds.Kind{
		Kind:     "demo",
		Plural:   "demos",
		SpecType: reflect.TypeFor[demoSpec](),
		Apply:    applyFn,
		Delete:   deleteFn,
	}
}

func multiDocYAML(docs ...string) string {
	return strings.Join(docs, "\n---\n")
}

func demoDoc(name, version string) string {
	return `apiVersion: ar.dev/v1alpha1
kind: demo
metadata:
  name: ` + name + `
  version: ` + version + `
spec:
  message: hello`
}

func parseResponse(t *testing.T, w *httptest.ResponseRecorder) applyResponse {
	t.Helper()
	var resp applyResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

// ---------- Tests ----------

func TestApplyMultiDocAllSucceed(t *testing.T) {
	fn := func(_ context.Context, doc *kinds.Document, _ kinds.ApplyOpts) (*kinds.Result, error) {
		return &kinds.Result{
			Kind: doc.Kind, Name: doc.Metadata.Name, Version: doc.Metadata.Version,
			Status: kinds.StatusApplied,
		}, nil
	}
	mux, _ := setupTestAPI(t, demoKind(fn, nil))

	body := multiDocYAML(demoDoc("a", "1.0.0"), demoDoc("b", "2.0.0"), demoDoc("c", "3.0.0"))
	req := httptest.NewRequest(http.MethodPost, "/v0/apply", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/yaml")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	resp := parseResponse(t, w)
	require.Len(t, resp.Results, 3)
	for _, r := range resp.Results {
		assert.Equal(t, kinds.StatusApplied, r.Status, "name=%s", r.Name)
	}
}

func TestApplyPartialFailure(t *testing.T) {
	callCount := 0
	fn := func(_ context.Context, doc *kinds.Document, _ kinds.ApplyOpts) (*kinds.Result, error) {
		callCount++
		if callCount == 2 {
			return nil, errors.New("boom")
		}
		return &kinds.Result{
			Kind: doc.Kind, Name: doc.Metadata.Name, Version: doc.Metadata.Version,
			Status: kinds.StatusApplied,
		}, nil
	}
	mux, _ := setupTestAPI(t, demoKind(fn, nil))

	body := multiDocYAML(demoDoc("a", "1"), demoDoc("b", "2"), demoDoc("c", "3"))
	req := httptest.NewRequest(http.MethodPost, "/v0/apply", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/yaml")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "HTTP status must be 200 even on partial failure")
	resp := parseResponse(t, w)
	require.Len(t, resp.Results, 3)

	assert.Equal(t, kinds.StatusApplied, resp.Results[0].Status)
	assert.Equal(t, kinds.StatusFailed, resp.Results[1].Status)
	assert.Contains(t, resp.Results[1].Error, "boom")
	assert.Equal(t, kinds.StatusApplied, resp.Results[2].Status)
}

func TestApplyUnknownKindReturnsPerDocFailure(t *testing.T) {
	fn := func(_ context.Context, doc *kinds.Document, _ kinds.ApplyOpts) (*kinds.Result, error) {
		return &kinds.Result{
			Kind: doc.Kind, Name: doc.Metadata.Name, Version: doc.Metadata.Version,
			Status: kinds.StatusApplied,
		}, nil
	}
	mux, _ := setupTestAPI(t, demoKind(fn, nil))

	unknownDoc := `apiVersion: ar.dev/v1alpha1
kind: unicorn
metadata:
  name: sparkle
spec:
  magic: true`
	body := multiDocYAML(demoDoc("a", "1"), unknownDoc)
	req := httptest.NewRequest(http.MethodPost, "/v0/apply", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/yaml")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	resp := parseResponse(t, w)
	require.Len(t, resp.Results, 2)

	// The unknown kind doc should be a per-doc failure.
	var unknownResult *kinds.Result
	var knownResult *kinds.Result
	for i := range resp.Results {
		if resp.Results[i].Kind == "unicorn" {
			unknownResult = &resp.Results[i]
		}
		if resp.Results[i].Kind == "demo" {
			knownResult = &resp.Results[i]
		}
	}
	require.NotNil(t, unknownResult)
	assert.Equal(t, kinds.StatusFailed, unknownResult.Status)
	assert.Contains(t, unknownResult.Error, "unknown kind")

	require.NotNil(t, knownResult)
	assert.Equal(t, kinds.StatusApplied, knownResult.Status)
}

func TestApplyMalformedBodyReturns400(t *testing.T) {
	fn := func(_ context.Context, _ *kinds.Document, _ kinds.ApplyOpts) (*kinds.Result, error) {
		return &kinds.Result{Status: kinds.StatusApplied}, nil
	}
	mux, _ := setupTestAPI(t, demoKind(fn, nil))

	body := `{{{not yaml or json`
	req := httptest.NewRequest(http.MethodPost, "/v0/apply", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/yaml")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.True(t, w.Code >= 400 && w.Code < 500, "expected 4xx, got %d", w.Code)
}

func TestDeleteDispatchesToDeleteFn(t *testing.T) {
	var mu sync.Mutex
	deleted := map[string]bool{}
	deleteFn := func(_ context.Context, name, version string) error {
		mu.Lock()
		deleted[name+"@"+version] = true
		mu.Unlock()
		return nil
	}
	mux, _ := setupTestAPI(t, demoKind(nil, deleteFn))

	body := multiDocYAML(demoDoc("x", "1.0"), demoDoc("y", "2.0"))
	req := httptest.NewRequest(http.MethodDelete, "/v0/apply", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/yaml")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	resp := parseResponse(t, w)
	require.Len(t, resp.Results, 2)
	for _, r := range resp.Results {
		assert.Equal(t, kinds.StatusDeleted, r.Status, "name=%s", r.Name)
	}

	mu.Lock()
	defer mu.Unlock()
	assert.True(t, deleted["x@1.0"])
	assert.True(t, deleted["y@2.0"])
}

func TestApplyPassesForceAndDryRunToApplyFn(t *testing.T) {
	var captured kinds.ApplyOpts
	fn := func(_ context.Context, doc *kinds.Document, opts kinds.ApplyOpts) (*kinds.Result, error) {
		captured = opts
		return &kinds.Result{
			Kind: doc.Kind, Name: doc.Metadata.Name, Version: doc.Metadata.Version,
			Status: kinds.StatusApplied,
		}, nil
	}
	mux, _ := setupTestAPI(t, demoKind(fn, nil))

	body := demoDoc("a", "1")
	req := httptest.NewRequest(http.MethodPost, "/v0/apply?force=true&dryRun=true", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/yaml")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, captured.Force, "Force should be true")
	assert.True(t, captured.DryRun, "DryRun should be true")
}

func TestApplyUnknownKindInDocDoesNotBlockSubsequentDocs(t *testing.T) {
	var appliedNames []string
	fn := func(_ context.Context, doc *kinds.Document, _ kinds.ApplyOpts) (*kinds.Result, error) {
		appliedNames = append(appliedNames, doc.Metadata.Name)
		return &kinds.Result{
			Kind: doc.Kind, Name: doc.Metadata.Name, Version: doc.Metadata.Version,
			Status: kinds.StatusApplied,
		}, nil
	}
	mux, _ := setupTestAPI(t, demoKind(fn, nil))

	unknownDoc := `apiVersion: ar.dev/v1alpha1
kind: ghost
metadata:
  name: casper
spec: {}`
	body := multiDocYAML(demoDoc("before", "1"), unknownDoc, demoDoc("after", "2"))
	req := httptest.NewRequest(http.MethodPost, "/v0/apply", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/yaml")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	resp := parseResponse(t, w)
	require.Len(t, resp.Results, 3)

	// Both known docs should have been applied.
	assert.Contains(t, appliedNames, "before")
	assert.Contains(t, appliedNames, "after")

	// The unknown doc should be failed.
	var ghostResult *kinds.Result
	for i := range resp.Results {
		if resp.Results[i].Kind == "ghost" {
			ghostResult = &resp.Results[i]
		}
	}
	require.NotNil(t, ghostResult)
	assert.Equal(t, kinds.StatusFailed, ghostResult.Status)
}
