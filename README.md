# AI Registry and Runtime - arctl

A CLI tool for managing MCP (Model Context Protocol) servers, skills, and registries with an embedded web UI.

## Quick Start

```bash
# Build the project
make build

# Run the CLI
./bin/arctl --help

# Launch the web UI
./bin/arctl ui
```

Then open http://localhost:8080 in your browser.


## CLI Commands

```
# Connects an existing public or private registry to arctl
# this will fetch the data from the registry and store it locally
arctl connect <registry-url> <registry-name>

# removes the cached data and the registry from the config
arctl disconnect <registry-name>

# List resources -- this lists the resources across the connected registries
arctl list <resource-type>

# Lists MCP servers from all connected registries
arctl list mcp

# Lists skills from all connected registries
arctl list skill

# Lists all connected registries
arctl list registry

# Search for resources from the connected registries
arctl search <resource-type> <search-term>

# Updates/fetches the new data from the connected registries
arctl refresh

# Shows details of a resource
arctl show <resource-type> <resource-name>
arctl show mcp <mcp-server-name>
arctl show skill <skill-name>
arctl show registry <registry-name>

# Install/uninstall resources 
arctl install mcp <mcp-server-name> <version> <config>
arctl install skill <skill-name> <version>

arctl uninstall mcp <mcp-server-name>
arctl uninstall skill <skill-name>

# Client configuration - creates the .json configuration for each client, so it can connect to the arctl
arctl configure <client-name>

# Starts/restarts the arctl with the existing configuration
arctl start

# Launches the UI
arctl ui

# Import servers into the registry database
arctl import --source ./seed.json
arctl import --source https://example.com/seed.json --request-header Authorization=Bearer:XXX
arctl import --source https://example.com/v0/servers --timeout 60s
arctl import --source https://github.com/org/registry-seed.json --github-token $GITHUB_TOKEN --update

# Export servers from the registry database
arctl export --output ./seed.json

# shows the version
arctl version
```
