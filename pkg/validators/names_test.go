package validators

import (
	"strings"
	"testing"
)

func TestValidateProjectName(t *testing.T) {
	tests := []struct {
		name        string
		projectName string
		wantErr     bool
		errContain  string
	}{
		{"valid name", "my-project", false, ""},
		{"valid with underscore", "my_project", false, ""},
		{"valid alphanumeric", "project123", false, ""},
		{"empty name", "", true, "cannot be empty"},
		{"name with space", "my project", true, "invalid characters"},
		{"name with slash", "my/project", true, "invalid characters"},
		{"name starting with dot", ".hidden", true, "cannot start with a dot"},
		{"name with colon", "my:project", true, "invalid characters"},
		{"name with asterisk", "my*project", true, "invalid characters"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProjectName(tt.projectName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateProjectName(%q) error = %v, wantErr %v",
					tt.projectName, err, tt.wantErr)
			}
			if tt.wantErr && tt.errContain != "" && err != nil {
				if !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("ValidateProjectName(%q) error = %v, want error containing %q",
						tt.projectName, err, tt.errContain)
				}
			}
		})
	}
}

func TestValidateAgentName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Valid DNS-1123 names
		{"valid simple", "myagent", false},
		{"valid alphanumeric", "agent123", false},
		{"valid single char", "a", false},
		{"valid with hyphen", "my-agent", false},

		// Invalid - violates DNS-1123
		{"uppercase rejected", "MyAgent2", true},
		{"dot not allowed", "my.agent", true},
		{"underscore not allowed", "my_agent", true},
		{"contains slash", "my/agent", true},
		{"contains space", "my agent", true},
		{"starts with hyphen", "-agent", true},
		{"trailing hyphen", "agent-", true},
		{"empty", "", true},

		// CLI-only: Python keywords are rejected even though they're DNS-1123 valid
		{"python keyword class", "class", true},
		{"python keyword import", "import", true},
		{"python keyword return", "return", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAgentName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAgentName(%q) error = %v, wantErr %v",
					tt.input, err, tt.wantErr)
			}
		})
	}
}

// dnsLabelCases is the shared positive/negative set every DNS-1123-only kind
// uses (skill, prompt, deployment, MCP server). Agent has its own set because
// Python keywords are rejected on top of DNS-1123.
var dnsLabelCases = []struct {
	name    string
	input   string
	wantErr bool
}{
	{"valid simple", "my-thing", false},
	{"valid alphanumeric", "thing123", false},
	{"valid single char", "a", false},
	{"empty", "", true},
	{"uppercase rejected", "MyThing", true},
	{"underscore rejected", "my_thing", true},
	{"contains dot", "my.thing", true},
	{"contains slash", "my/thing", true},
	{"contains space", "my thing", true},
	{"starts with hyphen", "-thing", true},
	{"trailing hyphen", "thing-", true},
}

func TestValidateSkillName(t *testing.T)      { runDNSLabelCases(t, ValidateSkillName) }
func TestValidatePromptName(t *testing.T)     { runDNSLabelCases(t, ValidatePromptName) }
func TestValidateDeploymentName(t *testing.T) { runDNSLabelCases(t, ValidateDeploymentName) }
func TestValidateMCPServerName(t *testing.T)  { runDNSLabelCases(t, ValidateMCPServerName) }

func runDNSLabelCases(t *testing.T, fn func(string) error) {
	t.Helper()
	for _, tt := range dnsLabelCases {
		t.Run(tt.name, func(t *testing.T) {
			err := fn(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validator(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}
