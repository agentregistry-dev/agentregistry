package agent

import (
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	agentmanifest "github.com/agentregistry-dev/agentregistry/internal/cli/agent/manifest"
)

// pythonServersFromResolved projects the resolved MCP server entries
// into the JSON shape the python mcp_tools template consumes
// (mcp-servers.json, loaded at agent startup).
func pythonServersFromResolved(servers []agentmanifest.ResolvedMCPServer) []common.PythonMCPServer {
	if len(servers) == 0 {
		return nil
	}

	out := make([]common.PythonMCPServer, 0, len(servers))
	for _, srv := range servers {
		entry := common.PythonMCPServer{
			Name: srv.Name,
			Type: srv.Type,
		}
		if srv.Type == "remote" {
			entry.URL = srv.URL
			if len(srv.Headers) > 0 {
				entry.Headers = srv.Headers
			}
		}
		// For Type=="command", the Python code derives the URL via
		// f"http://{server_name}:3000/mcp" — no URL needs to be supplied.
		out = append(out, entry)
	}
	return out
}
