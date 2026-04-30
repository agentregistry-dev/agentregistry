package v1alpha1

import (
	"errors"
	"testing"
)

func TestRemoteAgent_DecodeRoundTrip(t *testing.T) {
	doc := []byte(`
apiVersion: ar.dev/v1alpha1
kind: RemoteAgent
metadata:
  name: summarizer
  version: "1.0.0"
spec:
  title: Summarizer (remote)
  description: Hosted summarizer agent
  remote:
    type: a2a
    url: https://example.test/agent
`)
	obj, err := Default.Decode(doc)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	r, ok := obj.(*RemoteAgent)
	if !ok {
		t.Fatalf("want *RemoteAgent, got %T", obj)
	}
	if r.APIVersion != GroupVersion || r.Kind != KindRemoteAgent {
		t.Fatalf("envelope mismatch: %+v", r.TypeMeta)
	}
	if r.Spec.Remote.Type != "a2a" || r.Spec.Remote.URL != "https://example.test/agent" {
		t.Fatalf("spec.remote mismatch: %+v", r.Spec.Remote)
	}
}

func TestRemoteAgent_Validate(t *testing.T) {
	cases := []struct {
		name    string
		obj     RemoteAgent
		wantErr bool
		wantSub string
	}{
		{
			name: "ok",
			obj: RemoteAgent{
				Metadata: ObjectMeta{Namespace: "default", Name: "summarizer", Version: "1"},
				Spec: RemoteAgentSpec{
					Remote: AgentRemote{Type: "a2a", URL: "https://example.test/agent"},
				},
			},
		},
		{
			name: "missing remote.type",
			obj: RemoteAgent{
				Metadata: ObjectMeta{Namespace: "default", Name: "summarizer", Version: "1"},
				Spec: RemoteAgentSpec{
					Remote: AgentRemote{URL: "https://example.test/agent"},
				},
			},
			wantErr: true,
			wantSub: "spec.remote.type",
		},
		{
			name: "missing remote.url",
			obj: RemoteAgent{
				Metadata: ObjectMeta{Namespace: "default", Name: "summarizer", Version: "1"},
				Spec: RemoteAgentSpec{
					Remote: AgentRemote{Type: "a2a"},
				},
			},
			wantErr: true,
			wantSub: "spec.remote.url",
		},
		{
			name: "empty remote",
			obj: RemoteAgent{
				Metadata: ObjectMeta{Namespace: "default", Name: "summarizer", Version: "1"},
				Spec:     RemoteAgentSpec{},
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
					if fieldErr.Path == tc.wantSub || (len(tc.wantSub) <= len(fieldErr.Path) && fieldErr.Path[:len(tc.wantSub)] == tc.wantSub) {
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
