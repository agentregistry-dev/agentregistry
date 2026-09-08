package registry

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/crud"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/secret"
	secretdatabase "github.com/agentregistry-dev/agentregistry/pkg/secret/database"
	secretkubernetes "github.com/agentregistry-dev/agentregistry/pkg/secret/kubernetes"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

type testSecretStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (*testSecretStore) Type() secret.StoreType { return secret.StoreTypeDatabase }
func (s *testSecretStore) Put(_ context.Context, _, _ string, data map[string][]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = data
	return nil
}
func (s *testSecretStore) Get(context.Context, string, string) (map[string][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		return nil, secret.ErrPayloadNotFound
	}
	return s.data, nil
}
func (s *testSecretStore) Delete(context.Context, string, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = nil
	return nil
}

type testSecretMetadataStore struct {
	existing *v1alpha1.RawObject
	status   json.RawMessage
}

type memorySecretPayloadPersistence struct {
	data map[string][]byte
}

func (p *memorySecretPayloadPersistence) UpsertSecretPayload(_ context.Context, namespace, name string, ciphertext []byte) error {
	p.data[namespace+"/"+name] = append([]byte(nil), ciphertext...)
	return nil
}

func (p *memorySecretPayloadPersistence) GetSecretPayload(_ context.Context, namespace, name string) ([]byte, error) {
	ciphertext, ok := p.data[namespace+"/"+name]
	if !ok {
		return nil, pkgdb.ErrNotFound
	}
	return append([]byte(nil), ciphertext...), nil
}

func (p *memorySecretPayloadPersistence) DeleteSecretPayload(_ context.Context, namespace, name string) error {
	delete(p.data, namespace+"/"+name)
	return nil
}

func (s *testSecretMetadataStore) GetLatest(context.Context, string, string) (*v1alpha1.RawObject, error) {
	if s.existing == nil {
		return nil, pkgdb.ErrNotFound
	}
	return s.existing, nil
}

func (s *testSecretMetadataStore) PatchStatus(_ context.Context, _, _, _ string, mutate func(json.RawMessage) (json.RawMessage, error)) error {
	status, err := mutate(s.status)
	s.status = status
	return err
}

func TestBuildSecretStore(t *testing.T) {
	supplied := &testSecretStore{}
	got, err := buildSecretStore(&config.Config{SecretStore: "invalid"}, nil, supplied)
	require.NoError(t, err)
	require.Same(t, supplied, got)

	got, err = buildSecretStore(&config.Config{}, nil, nil)
	require.NoError(t, err)
	require.Nil(t, got)

	_, err = buildSecretStore(&config.Config{SecretStore: string(secret.StoreTypeDatabase), SecretStoreEncryptionKey: "bad"}, nil, nil)
	require.ErrorContains(t, err, "SECRET_STORE_ENCRYPTION_KEY")
}

func TestSecretLifecycleHooks(t *testing.T) {
	payload := &testSecretStore{}
	service := secret.NewService(payload)
	tx := &secretPayloadTransactions{store: payload}
	metadata := &testSecretMetadataStore{}
	value := &v1alpha1.Secret{
		Metadata: v1alpha1.ObjectMeta{Name: "credentials"},
		Spec:     v1alpha1.SecretSpec{StringData: map[string]string{"token": "plaintext"}},
	}

	require.NoError(t, secretPrepare(service, metadata, tx)(t.Context(), value))
	require.Equal(t, []byte("plaintext"), payload.data["token"])
	require.Empty(t, value.Spec.StringData)
	require.Equal(t, []string{"token"}, value.Status.DataKeys)

	require.NoError(t, secretStatusPostUpsert(metadata)(t.Context(), value))
	require.JSONEq(t, `{"dataKeys":["token"]}`, string(metadata.status))

	require.NoError(t, secretPostDelete(service)(t.Context(), value))
	require.Nil(t, payload.data)
}

func TestSecretLifecycleWithConcreteStores(t *testing.T) {
	tests := []struct {
		name     string
		newStore func(*testing.T) secret.Store
	}{
		{
			name: "database",
			newStore: func(*testing.T) secret.Store {
				persistence := &memorySecretPayloadPersistence{data: map[string][]byte{}}
				return secretdatabase.New(persistence, secret.NewStaticKeyProvider(make([]byte, 32)))
			},
		},
		{
			name: "kubernetes",
			newStore: func(*testing.T) secret.Store {
				return secretkubernetes.New(k8sfake.NewSimpleClientset().CoreV1(), "agentregistry-system")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := test.newStore(t)
			service := secret.NewService(store)
			transactions := &secretPayloadTransactions{store: store}
			metadata := &testSecretMetadataStore{}
			prepare := secretPrepare(service, metadata, transactions)
			postUpsert := secretStatusPostUpsert(metadata)
			postDelete := secretPostDelete(service)

			created := &v1alpha1.Secret{
				Metadata: v1alpha1.ObjectMeta{Name: "credentials"},
				Spec: v1alpha1.SecretSpec{
					Data:       map[string]string{"certificate": "Y2VydGlmaWNhdGU="},
					StringData: map[string]string{"token": "initial"},
				},
			}
			require.NoError(t, prepare(t.Context(), created))
			require.Empty(t, created.Spec.Data)
			require.Empty(t, created.Spec.StringData)
			require.Equal(t, []string{"certificate", "token"}, created.Status.DataKeys)
			require.NoError(t, postUpsert(t.Context(), created))
			require.JSONEq(t, `{"dataKeys":["certificate","token"]}`, string(metadata.status))
			require.NoError(t, transactions.commit(t.Context(), created))

			token, err := service.Resolve(t.Context(), v1alpha1.SecretRef{Name: "credentials", Key: "token"})
			require.NoError(t, err)
			require.Equal(t, []byte("initial"), token.Reveal())
			require.Equal(t, "[REDACTED]", token.String())
			all, err := service.ResolveAll(t.Context(), v1alpha1.SecretRef{Name: "credentials"})
			require.NoError(t, err)
			require.Equal(t, []byte("certificate"), all["certificate"].Reveal())
			require.Equal(t, []byte("initial"), all["token"].Reveal())
			_, err = service.Resolve(t.Context(), v1alpha1.SecretRef{Name: "credentials", Key: "missing"})
			require.ErrorIs(t, err, secret.ErrNotFound)

			updated := &v1alpha1.Secret{
				Metadata: v1alpha1.ObjectMeta{Name: "credentials"},
				Spec:     v1alpha1.SecretSpec{StringData: map[string]string{"token": "updated"}},
			}
			require.NoError(t, prepare(t.Context(), updated))
			require.NoError(t, transactions.commit(t.Context(), updated))
			token, err = service.Resolve(t.Context(), v1alpha1.SecretRef{Name: "credentials", Key: "token"})
			require.NoError(t, err)
			require.Equal(t, []byte("updated"), token.Reveal())

			failedUpdate := &v1alpha1.Secret{
				Metadata: v1alpha1.ObjectMeta{Name: "credentials"},
				Spec:     v1alpha1.SecretSpec{StringData: map[string]string{"token": "rolled-back"}},
			}
			require.NoError(t, prepare(t.Context(), failedUpdate))
			require.NoError(t, transactions.rollback(t.Context(), failedUpdate, errors.New("metadata failed")))
			token, err = service.Resolve(t.Context(), v1alpha1.SecretRef{Name: "credentials", Key: "token"})
			require.NoError(t, err)
			require.Equal(t, []byte("updated"), token.Reveal())

			failedCreate := &v1alpha1.Secret{
				Metadata: v1alpha1.ObjectMeta{Name: "orphan"},
				Spec:     v1alpha1.SecretSpec{StringData: map[string]string{"token": "temporary"}},
			}
			require.NoError(t, prepare(t.Context(), failedCreate))
			require.NoError(t, transactions.rollback(t.Context(), failedCreate, errors.New("metadata failed")))
			_, err = service.ResolveAll(t.Context(), v1alpha1.SecretRef{Name: "orphan"})
			require.ErrorIs(t, err, secret.ErrNotFound)

			require.NoError(t, postDelete(t.Context(), updated))
			_, err = service.ResolveAll(t.Context(), v1alpha1.SecretRef{Name: "credentials"})
			require.ErrorIs(t, err, secret.ErrNotFound)
			require.NoError(t, postDelete(t.Context(), updated))
		})
	}
}

func TestSecretPayloadRollback(t *testing.T) {
	t.Run("restores existing payload", func(t *testing.T) {
		payload := &testSecretStore{data: map[string][]byte{"token": []byte("old")}}
		tx := &secretPayloadTransactions{store: payload}
		value := &v1alpha1.Secret{Metadata: v1alpha1.ObjectMeta{Name: "credentials"}}

		require.NoError(t, tx.snapshot(t.Context(), value, v1alpha1.DefaultNamespace))
		require.NoError(t, payload.Put(t.Context(), v1alpha1.DefaultNamespace, "credentials", map[string][]byte{"token": []byte("new")}))
		require.NoError(t, tx.rollback(t.Context(), value, errors.New("metadata failed")))
		require.Equal(t, []byte("old"), payload.data["token"])
	})

	t.Run("deletes newly created payload", func(t *testing.T) {
		payload := &testSecretStore{}
		tx := &secretPayloadTransactions{store: payload}
		value := &v1alpha1.Secret{Metadata: v1alpha1.ObjectMeta{Name: "credentials"}}

		require.NoError(t, tx.snapshot(t.Context(), value, v1alpha1.DefaultNamespace))
		require.NoError(t, payload.Put(t.Context(), v1alpha1.DefaultNamespace, "credentials", map[string][]byte{"token": []byte("new")}))
		require.NoError(t, tx.rollback(t.Context(), value, errors.New("metadata failed")))
		require.Nil(t, payload.data)
	})
}

func TestSecretPayloadRollbackSerializesConcurrentApply(t *testing.T) {
	payload := &testSecretStore{data: map[string][]byte{"token": []byte("old")}}
	tx := &secretPayloadTransactions{store: payload}
	first := &v1alpha1.Secret{Metadata: v1alpha1.ObjectMeta{Name: "credentials"}}
	second := &v1alpha1.Secret{Metadata: v1alpha1.ObjectMeta{Name: "credentials"}}
	require.NoError(t, tx.snapshot(t.Context(), first, v1alpha1.DefaultNamespace))
	require.NoError(t, payload.Put(t.Context(), v1alpha1.DefaultNamespace, "credentials", map[string][]byte{"token": []byte("first")}))

	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		if err := tx.snapshot(t.Context(), second, v1alpha1.DefaultNamespace); err != nil {
			done <- err
			return
		}
		if err := payload.Put(t.Context(), v1alpha1.DefaultNamespace, "credentials", map[string][]byte{"token": []byte("second")}); err != nil {
			done <- err
			return
		}
		done <- tx.commit(t.Context(), second)
	}()
	<-started

	require.NoError(t, tx.rollback(t.Context(), first, errors.New("metadata failed")))
	require.NoError(t, <-done)
	require.Equal(t, []byte("second"), payload.data["token"])
}

func TestWireSecretServiceWithoutPayloadStoreFailsFast(t *testing.T) {
	hooks := &crud.PerKindHooks{}
	wireSecretService(hooks, map[string]*v1alpha1store.Store{v1alpha1.KindSecret: {}}, nil)

	err := hooks.Prepares[v1alpha1.KindSecret](t.Context(), &v1alpha1.Secret{})
	require.ErrorContains(t, err, "secret payload store is not configured")
}

func TestWireSecretServicePreservesUpsertErrorHook(t *testing.T) {
	payload := &testSecretStore{}
	called := false
	hooks := &crud.PerKindHooks{OnUpsertErrors: map[string]func(context.Context, v1alpha1.Object, error) error{
		v1alpha1.KindSecret: func(context.Context, v1alpha1.Object, error) error {
			called = true
			return nil
		},
	}}
	wireSecretService(hooks, map[string]*v1alpha1store.Store{v1alpha1.KindSecret: {}}, payload)
	value := &v1alpha1.Secret{Metadata: v1alpha1.ObjectMeta{Name: "credentials"}}

	require.NoError(t, hooks.OnUpsertErrors[v1alpha1.KindSecret](t.Context(), value, errors.New("metadata failed")))
	require.True(t, called)
}

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
	}, nil)
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

// TestCrudPerKindHooksCopiesAuthorizeInput pins the adapter's field-for-field
// contract: every AuthorizeInput field reaches the public hooks, Object included.
func TestCrudPerKindHooksCopiesAuthorizeInput(t *testing.T) {
	obj := &v1alpha1.Deployment{
		Metadata: v1alpha1.ObjectMeta{Namespace: "prod", Name: "checkout"},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef: v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "solo-docs"},
		},
	}
	in := resource.AuthorizeInput{
		Verb:      "apply",
		Kind:      v1alpha1.KindDeployment,
		Namespace: "prod",
		Name:      "checkout",
		Tag:       "v1",
		Object:    obj,
	}
	want := types.AuthorizeInput{
		Verb:      "apply",
		Kind:      v1alpha1.KindDeployment,
		Namespace: "prod",
		Name:      "checkout",
		Tag:       "v1",
		Object:    obj,
	}

	var gotAuthorizer, gotListFilter types.AuthorizeInput
	hooks := crudPerKindHooks(types.AppOptions{
		Authorizers: map[string]types.Authorizer{
			v1alpha1.KindDeployment: func(_ context.Context, got types.AuthorizeInput) error {
				gotAuthorizer = got
				return nil
			},
		},
		ListFilters: map[string]types.ListFilter{
			v1alpha1.KindDeployment: func(_ context.Context, got types.AuthorizeInput) (string, []any, error) {
				gotListFilter = got
				return "", nil, nil
			},
		},
	})

	require.NoError(t, hooks.Authorizers[v1alpha1.KindDeployment](t.Context(), in))
	_, _, err := hooks.ListFilters[v1alpha1.KindDeployment](t.Context(), in)
	require.NoError(t, err)

	assert.Equal(t, want, gotAuthorizer)
	assert.Equal(t, want, gotListFilter)
}
