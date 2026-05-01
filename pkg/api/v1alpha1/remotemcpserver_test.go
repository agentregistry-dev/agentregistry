package v1alpha1

import (
	"errors"
	"strings"
	"testing"
)

func TestRemoteMCPServer_DecodeRoundTrip(t *testing.T) {
	doc := []byte(`
apiVersion: ar.dev/v1alpha1
kind: RemoteMCPServer
metadata:
  name: weather
  version: "1.0.0"
spec:
  title: Weather (remote)
  description: Hosted weather endpoint
  remote:
    type: streamable-http
    url: https://example.test/mcp
`)
	obj, err := Default.Decode(doc)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	r, ok := obj.(*RemoteMCPServer)
	if !ok {
		t.Fatalf("want *RemoteMCPServer, got %T", obj)
	}
	if r.APIVersion != GroupVersion || r.Kind != KindRemoteMCPServer {
		t.Fatalf("envelope mismatch: %+v", r.TypeMeta)
	}
	if r.Spec.Remote.Type != "streamable-http" || r.Spec.Remote.URL != "https://example.test/mcp" {
		t.Fatalf("spec.remote mismatch: %+v", r.Spec.Remote)
	}
}

func TestRemoteMCPServer_Validate(t *testing.T) {
	cases := []struct {
		name    string
		obj     RemoteMCPServer
		wantErr bool
		wantSub string
	}{
		{
			name: "ok",
			obj: RemoteMCPServer{
				Metadata: ObjectMeta{Namespace: "default", Name: "weather", Version: "1"},
				Spec: RemoteMCPServerSpec{
					Remote: MCPTransport{Type: "streamable-http", URL: "https://example.test/mcp"},
				},
			},
		},
		{
			name: "missing remote.type",
			obj: RemoteMCPServer{
				Metadata: ObjectMeta{Namespace: "default", Name: "weather", Version: "1"},
				Spec: RemoteMCPServerSpec{
					Remote: MCPTransport{URL: "https://example.test/mcp"},
				},
			},
			wantErr: true,
			wantSub: "spec.remote.type",
		},
		{
			name: "missing remote.url",
			obj: RemoteMCPServer{
				Metadata: ObjectMeta{Namespace: "default", Name: "weather", Version: "1"},
				Spec: RemoteMCPServerSpec{
					Remote: MCPTransport{Type: "streamable-http"},
				},
			},
			wantErr: true,
			wantSub: "spec.remote.url",
		},
		{
			name: "non-https rejected",
			obj: RemoteMCPServer{
				Metadata: ObjectMeta{Namespace: "default", Name: "weather", Version: "1"},
				Spec: RemoteMCPServerSpec{
					Remote: MCPTransport{Type: "streamable-http", URL: "http://example.test/mcp"},
				},
			},
			wantErr: true,
			wantSub: "spec.remote.url",
		},
		{
			name: "empty remote",
			obj: RemoteMCPServer{
				Metadata: ObjectMeta{Namespace: "default", Name: "weather", Version: "1"},
				Spec:     RemoteMCPServerSpec{},
			},
			wantErr: true,
			wantSub: "spec.remote",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.obj.Validate()
			if tc.wantErr {
				var fe FieldErrors
				if !errors.As(err, &fe) {
					t.Fatalf("want FieldErrors, got %T (%v)", err, err)
				}
				found := false
				for _, fieldErr := range fe {
					if fieldErr.Path == tc.wantSub || strings.HasPrefix(fieldErr.Path, tc.wantSub) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("want path containing %q, got %v", tc.wantSub, fe)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
}
