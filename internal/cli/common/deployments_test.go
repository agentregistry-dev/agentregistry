package common

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestDeploymentRecordFromObject_RuntimeMetadata(t *testing.T) {
	tests := []struct {
		name        string
		status      map[string]string
		annotations map[string]string
		want        map[string]any
	}{
		{
			name:   "status details",
			status: map[string]string{"remoteId": "status-id"},
			annotations: map[string]string{
				"runtimes.agentregistry.solo.io/test/remoteId": "annotation-id",
			},
			want: map[string]any{"remoteId": "status-id"},
		},
		{
			name: "legacy annotation fallback",
			annotations: map[string]string{
				"runtimes.agentregistry.solo.io/test/remoteId": "annotation-id",
			},
			want: map[string]any{"remoteId": "annotation-id"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deployment := &v1alpha1.Deployment{}
			deployment.Metadata.Annotations = tt.annotations
			if tt.status != nil {
				if err := deployment.Status.SetDetailsKey(runtimeMetadataDetailsKey, tt.status); err != nil {
					t.Fatalf("SetDetailsKey() error = %v", err)
				}
			}

			got := DeploymentRecordFromObject(deployment).RuntimeMetadata
			if !maps.Equal(got, tt.want) {
				t.Fatalf("RuntimeMetadata = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestListDeploymentsDefaultsOriginManaged(t *testing.T) {
	var rawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/v0/deployments" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		rawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	}))
	t.Cleanup(srv.Close)

	_, err := ListDeployments(context.Background(), client.NewClient(srv.URL, ""))
	if err != nil {
		t.Fatalf("ListDeployments() error = %v", err)
	}

	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		t.Fatalf("ParseQuery(%q) error = %v", rawQuery, err)
	}
	if got := query.Get("origin"); got != deploymentOriginManaged {
		t.Fatalf("origin query = %q, want %q", got, deploymentOriginManaged)
	}
}
