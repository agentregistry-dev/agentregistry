package commands_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/cli/commands"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestNewExtensionKindUsesDescriptorStorage(t *testing.T) {
	tests := []struct {
		name       string
		kind       string
		wantPlural string
		wantTagged bool
	}{
		{name: "tagged", kind: v1alpha1.KindAgent, wantPlural: "agents", wantTagged: true},
		{name: "untagged", kind: v1alpha1.KindRuntime, wantPlural: "runtimes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind := commands.NewExtensionKind(commands.ExtensionKind{
				Name:          "extension-" + tt.name,
				CanonicalKind: tt.kind,
			})

			assert.Equal(t, tt.wantPlural, kind.Plural)
			require.NotNil(t, kind.Get)
			require.NotNil(t, kind.ListFunc)
			require.NotNil(t, kind.Delete)
			if tt.wantTagged {
				require.NotNil(t, kind.ListTags)
				require.NotNil(t, kind.DeleteAllTags)
			} else {
				require.Nil(t, kind.ListTags)
				require.Nil(t, kind.DeleteAllTags)
			}
		})
	}
}

func TestNewExtensionKindUsesDescriptorRoutes(t *testing.T) {
	tests := []struct {
		name     string
		kind     string
		tag      string
		response v1alpha1.Object
		wantURI  string
	}{
		{
			name: "tagged",
			kind: v1alpha1.KindAgent,
			tag:  "stable",
			response: &v1alpha1.Agent{
				TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
				Metadata: v1alpha1.ObjectMeta{Namespace: "team-a", Name: "example", Tag: "stable"},
			},
			wantURI: "/v0/agents/example/stable?namespace=team-a",
		},
		{
			name: "untagged",
			kind: v1alpha1.KindRuntime,
			tag:  "ignored",
			response: &v1alpha1.Runtime{
				TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindRuntime},
				Metadata: v1alpha1.ObjectMeta{Namespace: "team-a", Name: "example"},
			},
			wantURI: "/v0/runtimes/example?namespace=team-a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotURI string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotURI = r.URL.RequestURI()
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			t.Cleanup(srv.Close)

			kind := commands.NewExtensionKind(commands.ExtensionKind{
				Name:          "extension-" + tt.name,
				CanonicalKind: tt.kind,
			})
			_, err := kind.Get(context.Background(), client.NewClient(srv.URL, ""), "team-a/example", tt.tag)
			require.NoError(t, err)
			assert.Equal(t, tt.wantURI, gotURI)
		})
	}
}
