# Agent Registry

> **A comprehensive platform for discovering, deploying, and managing MCP (Model Context Protocol) servers, agents and skills**

Agent Registry is a unified system that combines a centralized registry, runtime management, and development tooling for MCP servers, agents and skills. It enables teams to publish, discover, and deploy AI agent capabilities as composable services.

[![Go Version](https://img.shields.io/badge/Go-1.25%2B-blue.svg)](https://golang.org/doc/install)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

## 🎯 What is Agent Registry?

Agent Registry solves the challenge of managing AI agent capabilities by providing:

- **📦 Centralized Registry**: Discover and publish MCP servers, skills, and agents
- **🚀 Automated Deployment**: Deploy MCP servers locally or remotely with one command
- **🔧 Developer Tools**: Scaffolding and code generators for Python and Go MCP servers
- **🌐 Web UI**: Beautiful dashboard for managing your agent ecosystem
- **🔌 Agent Gateway**: Unified endpoint for all your MCP servers
- **🐳 Container Orchestration**: Automated Docker Compose management

## 🏗️ Architecture

### Operation

![Architecture](img/operator-scenario.png)

### Development

![Architecture](img/dev-scenario.png)

## 🚀 Quick Start

### Prerequisites

- Docker Desktop with Docker Compose v2+
- Go 1.25+ (for building from source)

### Installation

```bash
# Install via script (recommended)
curl -fsSL https://raw.githubusercontent.com/agentregistry-dev/agentregistry/main/scripts/get-arctl | bash

# Or download binary directly from releases
# https://github.com/agentregistry-dev/agentregistry/releases
```

### Start the Registry

```bash
# Start the registry server and look for available MCP servers
arctl mcp list

# The first time the CLI runs it will automatically start the registry server daemon and import the built-in seed data.
```


### Access the Web UI

```bash
# Launch the embedded web interface
arctl ui

# Open http://localhost:8080 in your browser
```

## 📚 Core Concepts

### MCP Servers

MCP (Model Context Protocol) servers are services that provide tools, resources, and prompts to AI agents. They're the building blocks of agent capabilities.

### Agent Gateway

The [Agent Gateway](https://github.com/agentgateway/agentgateway) is a reverse proxy that provides a single MCP endpoint for all deployed servers:

```mermaid
sequenceDiagram
    participant IDE as AI IDE/Client
    participant GW as Agent Gateway
    participant FS as filesystem MCP
    participant GH as github MCP
    
    IDE->>GW: Connect (MCP over HTTP)
    GW-->>IDE: Available tools from all servers
    
    IDE->>GW: Call read_file()
    GW->>FS: Forward to filesystem
    FS-->>GW: File contents
    GW-->>IDE: Return result
    
    IDE->>GW: Call create_issue()
    GW->>GH: Forward to github
    GH-->>GW: Issue created
    GW-->>IDE: Return result
```

### IDE Configuration

Configure your AI-powered IDEs to use the Agent Gateway:

```bash
# Generate Claude Desktop config
arctl configure claude-desktop

# Generate Cursor config
arctl configure cursor

# Generate VS Code config
arctl configure vscode
```



## 🎨 Web UI

The embedded web interface provides a visual dashboard for:

- 📊 **Dashboard**: Overview of servers, deployments, and statistics
- 🔍 **Discovery**: Browse and search the registry
- 🚀 **Deployments**: Visual deployment management
- ⚙️ **Configuration**: Server settings and environment variables
- 📈 **Monitoring**: Deployment status and health


## 🤝 Contributing

We welcome contributions! Please see [`CONTRIBUTING.md`](CONTRIBUTING.md) for guidelines.

**Development setup:**

See [`DEVELOPMENT.md`](DEVELOPMENT.md) for detailed architecture information.

## 📄 License

MIT License - see [`LICENSE`](LICENSE) for details.

## 🔗 Related Projects

- [Model Context Protocol](https://modelcontextprotocol.io/)
- [kagent](https://github.com/kagent-dev/kagent)
- [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)
- [FastMCP](https://github.com/jlowin/fastmcp)

## 📞 Support

- 📖 [Documentation](https://agentregistry.dev/docs)
- 💬 [GitHub Discussions](https://github.com/agentregistry-dev/agentregistry/discussions)
- 🐛 [Issue Tracker](https://github.com/agentregistry-dev/agentregistry/issues)

---

**Built with ❤️ for the AI agent community**
