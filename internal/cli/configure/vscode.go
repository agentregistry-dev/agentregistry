package configure

import (
	"encoding/json"
	"fmt"
)

// VSCodeConfigurer handles VS Code MCP configuration
type VSCodeConfigurer struct{}

// vscodeTokenInputID identifies the arctl token prompt in the top-level inputs
// array of .vscode/mcp.json. VS Code caches the prompted secret keyed by this
// id, so it must stay stable across runs.
const vscodeTokenInputID = "arctl-mcp-token"

// vscodeServerConfig is the arctl server entry written into .vscode/mcp.json.
// Other servers' entries are preserved verbatim by mergeServerEntry.
type vscodeServerConfig struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (v *VSCodeConfigurer) GetConfigPath() (string, error) {
	return ".vscode/mcp.json", nil
}

func (v *VSCodeConfigurer) CreateConfig(opts CreateOptions, configPath string) (any, error) {
	entry := vscodeServerConfig{
		Type: "http",
		URL:  opts.URL,
	}
	if opts.TokenEnv != "" {
		// VS Code prompts for ${input:...} values once and keeps them in its
		// secret storage; the token never lands in the file.
		entry.Headers = map[string]string{
			"Authorization": fmt.Sprintf("Bearer ${input:%s}", vscodeTokenInputID),
		}
	}

	root, err := mergeServerEntry(configPath, "servers", entry)
	if err != nil {
		return nil, err
	}
	if opts.TokenEnv != "" {
		if err := ensureTokenInput(root); err != nil {
			return nil, err
		}
	}
	return root, nil
}

func (v *VSCodeConfigurer) GetClientName() string {
	return "Visual Studio Code"
}

// ensureTokenInput appends the arctl token prompt to the top-level inputs array
// unless an entry with vscodeTokenInputID already exists. The inputs array is
// shared by every server in the file, so existing entries must be preserved and
// not duplcated.
func ensureTokenInput(root map[string]json.RawMessage) error {
	var inputs []json.RawMessage
	if raw, ok := root["inputs"]; ok {
		if err := json.Unmarshal(raw, &inputs); err != nil {
			return fmt.Errorf("parsing \"inputs\": %w", err)
		}
	}

	for _, raw := range inputs {
		var entry struct {
			ID string `json:"id"`
		}
		// avoids duplicates
		if err := json.Unmarshal(raw, &entry); err == nil && entry.ID == vscodeTokenInputID {
			return nil
		}
	}

	inputJSON, err := json.Marshal(map[string]any{
		"type":        "promptString",
		"id":          vscodeTokenInputID,
		"description": "Bearer token for the arctl MCP server",
		"password":    true,
	})
	if err != nil {
		return fmt.Errorf("marshaling token input: %w", err)
	}
	inputs = append(inputs, inputJSON)

	inputsJSON, err := json.Marshal(inputs)
	if err != nil {
		return fmt.Errorf("marshaling \"inputs\": %w", err)
	}
	root["inputs"] = inputsJSON
	return nil
}
