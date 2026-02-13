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
		// Valid cases - letters and digits only, starts with letter, min 2 chars
		{"valid simple", "myagent", false},
		{"valid alphanumeric", "agent123", false},
		{"valid mixed case", "MyAgent2", false},
		{"valid two chars", "ab", false},

		// Invalid - special characters not allowed
		{"hyphen not allowed", "my-agent", true},
		{"dot not allowed", "my.agent", true},
		{"underscore not allowed", "my_agent", true},
		{"contains slash", "my/agent", true},
		{"contains space", "my agent", true},

		// Invalid - must start with letter
		{"starts with number", "123agent", true},
		{"starts with dot", ".agent", true},
		{"starts with hyphen", "-agent", true},

		// Invalid - too short
		{"single char", "a", true},
		{"empty", "", true},

		// Invalid - Python keywords
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

func TestValidateMCPServerName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid simple", "myorg/my-server", false},
		{"valid with dots", "my.org/my.server", false},
		{"valid with underscore in name", "myorg/my_server", false},
		{"valid mixed", "my-org.com/server-v1.0", false},
		{"valid alphanumeric", "org123/server456", false},
		{"empty", "", true},
		{"missing slash", "myorgserver", true},
		{"empty namespace", "/server", true},
		{"empty name", "myorg/", true},
		{"namespace too short", "a/server", true},
		{"name too short", "myorg/s", true},
		{"namespace starts with dot", ".org/server", true},
		{"namespace ends with dot", "org./server", true},
		{"name starts with dot", "myorg/.server", true},
		{"name ends with dot", "myorg/server.", true},
		{"namespace with underscore", "my_org/server", true},
		{"multiple slashes", "my/org/server", true},
		{"namespace with space", "my org/server", true},
		{"name with space", "myorg/my server", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMCPServerName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateMCPServerName(%q) error = %v, wantErr %v",
					tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateSkillName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid simple", "my-skill", false},
		{"valid with underscore", "my_skill", false},
		{"valid alphanumeric", "skill123", false},
		{"valid mixed", "my-skill_v1", false},
		{"empty", "", true},
		{"contains dot", "my.skill", true},
		{"contains slash", "my/skill", true},
		{"contains space", "my skill", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSkillName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSkillName(%q) error = %v, wantErr %v",
					tt.input, err, tt.wantErr)
			}
		})
	}
}
