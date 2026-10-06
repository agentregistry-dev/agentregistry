package bundle

import (
	"bytes"
	"encoding/json"
	"maps"
	"net"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// agentPluginsInventory lists an Agent Plugins bundle's skills, valid mcp.json
// servers, and bin/ executables.
func agentPluginsInventory(b *CanonicalBundle) *v1alpha1.PluginInventory {
	return &v1alpha1.PluginInventory{
		Skills:      skills(b, isSkillsChild),
		MCPServers:  agentPluginsMCPServers(b),
		Executables: executables(b),
	}
}

// agentPluginsMCPSchema is the $schema an Agent Plugins mcp.json must declare.
const agentPluginsMCPSchema = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"

// agentPluginsMCP is an Agent Plugins mcp.json.
type agentPluginsMCP struct {
	Schema     string                     `json:"$schema"`
	MCPServers map[string]json.RawMessage `json:"mcpServers"`
}

// agentPluginsMCPServers returns the sorted names of the valid mcp.json servers.
func agentPluginsMCPServers(b *CanonicalBundle) []string {
	config, ok := decodeAgentPluginsMCP(b.Files["mcp.json"])
	if !ok {
		return nil
	}
	maps.DeleteFunc(config.MCPServers, func(_ string, raw json.RawMessage) bool {
		return !validAgentPluginsServer(raw)
	})
	return slices.Sorted(maps.Keys(config.MCPServers))
}

// decodeAgentPluginsMCP decodes mcp.json. ok is false when the file is absent
// or breaks the whole-file rules (§7.2.1), which disable every server.
func decodeAgentPluginsMCP(data []byte) (agentPluginsMCP, bool) {
	var config agentPluginsMCP
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&config)
	return config, err == nil && config.Schema == agentPluginsMCPSchema && config.MCPServers != nil
}

// agentPluginsServer is one mcp.json server entry, with its allowed fields and types.
type agentPluginsServer struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	CWD     string            `json:"cwd,omitempty"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

// validAgentPluginsServer reports whether raw is a valid server entry (§7.2.2).
// ponytail: checks the file only. A ./ command or cwd that passes through a
// symbolic link is not resolved on disk.
func validAgentPluginsServer(raw json.RawMessage) bool {
	var s agentPluginsServer
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&s) != nil {
		return false
	}
	switch s.Type {
	case "stdio":
		return s.validStdio()
	case "streamable-http", "sse":
		return s.validRemote()
	}
	return false
}

func (s agentPluginsServer) validStdio() bool {
	return s.URL == "" && s.Headers == nil &&
		validStdioCommand(s.Command) && validStdioEnv(s.Env) && validStdioCWD(s.CWD)
}

// validStdioCommand accepts a bare command or a ./ path with no "..".
func validStdioCommand(command string) bool {
	if command == "" || strings.ContainsAny(command, " \t\r\n") {
		return false
	}
	relative, pluginRelative := strings.CutPrefix(command, "./")
	if !pluginRelative {
		return !strings.Contains(command, "/")
	}
	return !path.IsAbs(relative) && !slices.Contains(strings.Split(relative, "/"), "..")
}

// validStdioEnv rejects an env that overrides PLUGIN_ROOT or PLUGIN_DATA.
func validStdioEnv(env map[string]string) bool {
	_, root := env["PLUGIN_ROOT"]
	_, data := env["PLUGIN_DATA"]
	return !root && !data
}

// validStdioCWD accepts a cwd inside the plugin root or its data directory.
func validStdioCWD(cwd string) bool {
	if cwd == "" {
		return true
	}
	if relative, ok := strings.CutPrefix(cwd, "./"); ok {
		return staysInside(relative)
	}
	for _, variable := range []string{"${PLUGIN_ROOT}", "${PLUGIN_DATA}"} {
		if rest, ok := strings.CutPrefix(cwd, variable); ok {
			return (rest == "" || strings.HasPrefix(rest, "/")) && staysInside(rest)
		}
	}
	return false
}

// staysInside reports whether relative, joined to a directory, stays inside it.
func staysInside(relative string) bool {
	cleaned := path.Clean(strings.TrimLeft(relative, "/"))
	return cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}

func (s agentPluginsServer) validRemote() bool {
	return s.Command == "" && s.Args == nil && s.Env == nil && s.CWD == "" && validRemoteURL(s.URL)
}

// validRemoteURL accepts an https URL, or an http URL to a loopback host.
func validRemoteURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if raw == "" || err != nil || parsed.User != nil || parsed.Fragment != "" || parsed.Host == "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	return parsed.Scheme == "http" && isLoopback(parsed.Hostname())
}

func isLoopback(host string) bool {
	return host == "localhost" || net.ParseIP(host).IsLoopback()
}
