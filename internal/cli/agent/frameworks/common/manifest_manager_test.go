package common

import (
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
)

func TestValidateRemoteMcpServer(t *testing.T) {
	tests := []struct {
		name    string
		srv     models.McpServerType
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid http url",
			srv:     models.McpServerType{Type: "remote", Name: "s", URL: "http://example.com/mcp"},
			wantErr: false,
		},
		{
			name:    "valid https url",
			srv:     models.McpServerType{Type: "remote", Name: "s", URL: "https://example.com/mcp"},
			wantErr: false,
		},
		{
			name:    "url with env var in host",
			srv:     models.McpServerType{Type: "remote", Name: "s", URL: "http://${GATEWAY_HOST}/mcp"},
			wantErr: false,
		},
		{
			name:    "url with env var in port",
			srv:     models.McpServerType{Type: "remote", Name: "s", URL: "http://localhost:${PORT}/mcp"},
			wantErr: false,
		},
		{
			name:    "url with multiple env vars",
			srv:     models.McpServerType{Type: "remote", Name: "s", URL: "https://${HOST}:${PORT}/${PATH}"},
			wantErr: false,
		},
		{
			name:    "url entirely from env var",
			srv:     models.McpServerType{Type: "remote", Name: "s", URL: "${FULL_URL}"},
			wantErr: true,
			errMsg:  "url scheme must be http or https",
		},
		{
			name:    "empty url",
			srv:     models.McpServerType{Type: "remote", Name: "s", URL: ""},
			wantErr: true,
			errMsg:  "url is required",
		},
		{
			name:    "missing scheme",
			srv:     models.McpServerType{Type: "remote", Name: "s", URL: "example.com/mcp"},
			wantErr: true,
			errMsg:  "url scheme must be http or https",
		},
		{
			name:    "ftp scheme",
			srv:     models.McpServerType{Type: "remote", Name: "s", URL: "ftp://example.com/mcp"},
			wantErr: true,
			errMsg:  "url scheme must be http or https",
		},
		{
			name:    "missing host",
			srv:     models.McpServerType{Type: "remote", Name: "s", URL: "http:///path"},
			wantErr: true,
			errMsg:  "url is missing host",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRemoteMcpServer(0, tt.srv)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errMsg)
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errMsg)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
