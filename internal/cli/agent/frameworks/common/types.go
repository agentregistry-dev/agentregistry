package common

// PythonMCPServer represents the JSON structure expected by the Python MCP tools template
// Equal to the type used in the python mcp_tools.py template.
type PythonMCPServer struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"` // "remote" or "command"
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}
