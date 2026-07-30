package configure

import (
	"encoding/json"
	"fmt"
	"os"
)

// serverName is the key arctl writes for its MCP server entry in every client config.
const serverName = "arctl"

// legacyServerName was written by earlier versions of the Cursor and Kiro
// configurers; it is removed in favor of serverName on rewrite. This maintains
// the same name across clients.
// This migration can be dropped once legacy-named configs are no longer expected in the wild.
const legacyServerName = "ARCTL"

// mergeServerEntry reads the JSON config at configPath (if present), replaces only
// <serversKey>[serverName] with entry, and returns the full document. All other
// content is preserved as raw JSON so fields arctl does not model survive the rewrite.
func mergeServerEntry(configPath, serversKey string, entry any) (map[string]json.RawMessage, error) {
	root := map[string]json.RawMessage{}
	if data, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(data, &root); err != nil {
			return nil, fmt.Errorf("parsing existing config: %w", err)
		}
	}

	servers := map[string]json.RawMessage{}
	if raw, ok := root[serversKey]; ok {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return nil, fmt.Errorf("parsing %q: %w", serversKey, err)
		}
	}

	entryJSON, err := json.Marshal(entry)
	if err != nil {
		return nil, fmt.Errorf("marshaling server entry: %w", err)
	}
	servers[serverName] = entryJSON

	// delete 'ARCTL' to prefer newer 'arctl' name
	delete(servers, legacyServerName)

	serversJSON, err := json.Marshal(servers)
	if err != nil {
		return nil, fmt.Errorf("marshaling %q: %w", serversKey, err)
	}
	root[serversKey] = serversJSON

	return root, nil
}
