<div align="center">
  <img src="img/agentregistry - Light on Transparent Background.svg" alt="Agent Registry" width="400">
  
  [![Go Version](https://img.shields.io/badge/Go-1.25%2B-blue.svg)](https://golang.org/doc/install)
  [![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
  [![Discord](https://img.shields.io/discord/1435836734666707190?label=Join%20Discord&logo=discord&logoColor=white&color=5865F2)](https://discord.gg/HTYNjF2y2t)
  
  ### A centralized registry to securely curate, discover, deploy, and manage agentic infrastructure from MCP servers, agents to skills.
</div>


##  What is Agent Registry?

Agent Registry brings governance and control to AI artifacts and infrastructure, empowering developers to quickly build and deploy AI applications with confidence. It provides a secure, centralized registry where teams can publish, discover, and share AI artifacts, including MCP servers, agents, and skills, and deploy them seamlessly to any environment.


### Agent Registry provides:

- **📦 Centralized Registry**: Package, discover and curate AI artifacts from a central source
- **🔒 Control and Governance**: Selectively  and control custom collection of artifacts
- **📊 Data Enrichment**: Automatically validate and score ingested data for insights
- **🌐 Unify AI Infrastructure**: Deploy and access artifacts anywhere

##  Agent Registry Architecture

### For Operators:  Enrich, package, curate and deploy with control
![Architecture](img/operator-scenario.png)

### For Developers: Build, push, pull and run applications with confidence

![Architecture](img/dev-scenario.png)

### Development setup

See [`DEVELOPMENT.md`](DEVELOPMENT.md) for detailed architecture information.

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
arctl list

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


## 🤝 Get Involved

### Contributing

We welcome contributions! Please see [`CONTRIBUTING.md`](CONTRIBUTING.md) for guidelines.


### Show your support

- 🐛 **Report bugs and issues**: [GitHub Issues](https://github.com/agentregistry-dev/agentregistry/issues)
- 💡 **Suggest new features**: [GitHub Discussions](https://github.com/agentregistry-dev/agentregistry/discussions)
- 🔧 **Submit pull requests**: [GitHub Repository](https://github.com/agentregistry-dev/agentregistry)
- ⭐ **Star the repository**: Show your support on [GitHub](https://github.com/agentregistry-dev/agentregistry)
- 💬 **Join the Conversation**: Join our [Discord Server](https://discord.gg/HTYNjF2y2t)

###  Related Projects

- [Model Context Protocol](https://modelcontextprotocol.io/)
- [kagent](https://github.com/kagent-dev/kagent)
- [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)
- [FastMCP](https://github.com/jlowin/fastmcp)

## 📚 Resources

- 📖 [Documentation] Coming Soon!
- 💬 [GitHub Discussions](https://github.com/agentregistry-dev/agentregistry/discussions)
- 🐛 [Issue Tracker](https://github.com/agentregistry-dev/agentregistry/issues)

## 📄 License

MIT License - see [`LICENSE`](LICENSE) for details.

---
