package manifest

import (
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestTranslateRemoteMCPServerRefAppliesHeaderDefaultsAndRequired(t *testing.T) {
	server := &v1alpha1.RemoteMCPServer{
		Metadata: v1alpha1.ObjectMeta{Name: "remote-tools"},
		Spec: v1alpha1.RemoteMCPServerSpec{
			Remote: v1alpha1.MCPTransport{
				Type: "streamable-http",
				URL:  "https://remote.example/mcp",
				Headers: []v1alpha1.MCPKeyValueInput{
					{Name: "Authorization", Value: "Bearer token", IsRequired: true},
					{Name: "X-Trace", Default: "trace-default"},
				},
			},
		},
	}

	got, err := translateRemoteMCPServerRef("tools", server)
	if err != nil {
		t.Fatalf("translateRemoteMCPServerRef: %v", err)
	}
	if got.URL != "https://remote.example/mcp" {
		t.Fatalf("URL = %q", got.URL)
	}
	if got.Headers["Authorization"] != "Bearer token" || got.Headers["X-Trace"] != "trace-default" {
		t.Fatalf("Headers = %+v", got.Headers)
	}
}

func TestTranslateRemoteMCPServerRefRejectsMissingRequiredHeader(t *testing.T) {
	server := &v1alpha1.RemoteMCPServer{
		Metadata: v1alpha1.ObjectMeta{Name: "remote-tools"},
		Spec: v1alpha1.RemoteMCPServerSpec{
			Remote: v1alpha1.MCPTransport{
				Type:    "streamable-http",
				URL:     "https://remote.example/mcp",
				Headers: []v1alpha1.MCPKeyValueInput{{Name: "Authorization", IsRequired: true}},
			},
		},
	}

	if _, err := translateRemoteMCPServerRef("tools", server); err == nil {
		t.Fatalf("expected missing required header error")
	}
}
