<div align="center">
  <picture>
    <img alt="agentregistry" src="./img/agentregistry-logo.svg" height="150"/>
  </picture>

  [![Go Version](https://img.shields.io/badge/Go-1.25%2B-blue.svg)](https://golang.org/doc/install)
  [![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
  [![Discord](https://img.shields.io/discord/1435836734666707190?label=Join%20Discord&logo=discord&logoColor=white&color=5865F2)](https://discord.gg/HTYNjF2y2t)

  ### Discover, curate, and deploy trusted MCP servers, agents, and skills from one registry.
</div>

Agent Registry gives platform teams and developers a single place to manage the AI artifacts their applications depend on.

Instead of every team finding, configuring, and exposing MCP servers on their own, Agent Registry helps you publish approved artifacts, discover what is available, and make those artifacts usable across local development and Kubernetes.

## Why Agent Registry?

- **Curate what teams can use**: Build a trusted catalog of MCP servers, agents, and skills instead of relying on ad hoc setup.
- **Speed up developer adoption**: Help developers discover approved AI building blocks quickly and start using them with minimal friction.
- **Standardize deployment**: Support local workflows, shared environments, and Kubernetes from the same registry.
- **Add control without slowing teams down**: Centralize discovery and access while keeping the developer experience simple.

## Quick Links

- [Install `arctl`](#install-arctl)
- [Start locally](#start-locally)
- [Deploy on Kubernetes](#deploy-on-kubernetes)
- [Contributing](CONTRIBUTING.md)
- [Development details](DEVELOPMENT.md)
- [Discord](https://discord.gg/HTYNjF2y2t)

## What You Can Do With Agent Registry

- **Publish and organize artifacts** in a central registry
- **Discover approved MCP servers, agents, and skills** from a shared catalog
- **Deploy artifacts across environments** from local development to Kubernetes
- **Configure AI clients and gateways** from a consistent source of truth
- **Create a safer path to production** for agentic infrastructure

## See It In Action

Learn how to create an Anthropic Skill, publish it to Agent Registry, and use it in Claude Code.

[![Video](https://img.youtube.com/vi/l6QicyGg46A/maxresdefault.jpg)](https://www.youtube.com/watch?v=l6QicyGg46A)

## Quick Start

### Install `arctl`

```bash
# Install via script (recommended)
curl -fsSL https://raw.githubusercontent.com/agentregistry-dev/agentregistry/main/scripts/get-arctl | bash

# Or download a binary from releases
# https://github.com/agentregistry-dev/agentregistry/releases
```

### Start locally

```bash
# Start the registry server and look for available MCP servers
arctl mcp list
```

The first time `arctl` runs, it automatically starts the local registry daemon and imports the built-in seed data.

### Open the UI

Visit `http://localhost:12121` in your browser.

### Configure your AI client

```bash
arctl configure claude-desktop
arctl configure cursor
arctl configure vscode
```

## Deploy Where Your Teams Work

### Local development

Use `arctl` and the web UI to discover artifacts, run workflows locally, and configure supported AI clients.

### Deploy on Kubernetes

Install Agent Registry on any Kubernetes cluster using the Helm chart. An external PostgreSQL instance with the [pgvector](https://github.com/pgvector/pgvector) extension is required.

#### PostgreSQL

Deploy a single-instance PostgreSQL and pgvector into your cluster using the provided example manifest:

```bash
kubectl apply -f https://raw.githubusercontent.com/agentregistry-dev/agentregistry/main/examples/postgres-pgvector.yaml
kubectl -n agentregistry wait --for=condition=ready pod -l app=postgres-pgvector --timeout=120s
```

This setup is intended for development and testing. For production, use a managed PostgreSQL service or a production-grade operator.

#### Agent Registry

```bash
helm install agentregistry oci://ghcr.io/agentregistry-dev/helm/agentregistry \
  --namespace agentregistry \
  --create-namespace \
  --set database.host=postgres-pgvector.agentregistry.svc.cluster.local \
  --set database.password=agentregistry \
  --set database.sslMode=disable \
  --set config.jwtPrivateKey=$(openssl rand -hex 32)
```

Then port-forward to access the UI:

```bash
kubectl port-forward -n agentregistry svc/agentregistry 12121:12121
```

For deployment details and configuration options, see [charts/agentregistry/README.md.gotmpl](charts/agentregistry/README.md.gotmpl) and [scripts/kind/README.md](scripts/kind/README.md).

## How Teams Use Agent Registry

### For platform teams

Curate, govern, and deploy approved AI artifacts with more control.

![Operator scenario](img/operator-scenario.png)

### For developers

Discover trusted building blocks and use them in real applications with less setup.

![Developer scenario](img/dev-scenario.png)

## Fits Into Your MCP Stack

Agent Registry works alongside the tools teams already use to build and operate agentic systems.

- MCP servers
- Agent Gateway
- Claude Desktop
- Cursor
- VS Code
- Kubernetes

## Contributing

We welcome contributions. See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution guidelines and [DEVELOPMENT.md](DEVELOPMENT.md) for architecture and development details.

## Community

- [GitHub Issues](https://github.com/agentregistry-dev/agentregistry/issues)
- [GitHub Discussions](https://github.com/agentregistry-dev/agentregistry/discussions)
- [Discord](https://discord.gg/HTYNjF2y2t)

## Related Projects

- [Model Context Protocol](https://modelcontextprotocol.io/)
- [Agent Gateway](https://github.com/agentgateway/agentgateway)
- [kagent](https://github.com/kagent-dev/kagent)
- [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)
- [FastMCP](https://github.com/jlowin/fastmcp)

## License

Apache V2 License. See [LICENSE](LICENSE) for details.
