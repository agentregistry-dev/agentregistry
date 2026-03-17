# General Technical Review — Agentregistry / Sandbox

_This document provides a General Technical Review of the agentregistry project. This is a living document that demonstrates to the Technical Advisory Group (TAG) that the project satisfies the Engineering Principle requirements for moving levels. This document follows the template outlined in the [TOC subproject review](https://github.com/cncf/toc/blob/main/toc_subprojects/project-reviews-subproject/general-technical-questions.md)_


> **Project:** agentregistry
> **Project Version:** v0.3.2
> **Website:** https://aregistry.ai
> **Date Updated:** 2026-03-17
> **Template Version:** v1.0
> **Description:** Agentregistry gives platform teams and developers one place to manage the agentic infrastructure their applications depend on.

---

## Day 0 — Planning Phase (Sandbox)

This section covers the design and architecture of Agentregistry as a cloud native project applying for CNCF Sandbox status.

---

### Scope

**Describe the project's roadmap.**

[TODO: Link to or summarize the project roadmap. The project is at v0.1.x (latest: v0.1.24, released February 2026) and is under active development (146 commits, 29 releases). A public roadmap documenting planned features, milestones, and the path to Incubation should be added.]


**What problem does this project solve?**

The rapid growth of AI agents, MCP (Model Context Protocol) servers, and skills has created a fragmented ecosystem with no standardized way to discover, curate, validate, or govern agentic infrastructure. Organizations face challenges such as:

- **No centralized, trusted source of truth** for AI artifacts (MCP servers, agents, skills).
- **Lack of governance controls** over which AI tools are approved for company-wide use.
- **Difficulty deploying** and managing AI artifacts consistently across multiple environments.
- **Absence of metadata enrichment**, scoring, or validation pipelines for agentic components.

agentregistry addresses these gaps by providing a centralized, secure registry where teams can publish, discover, curate, and deploy AI artifacts with confidence.

**What are the benefits of solving this problem in a cloud native manner?**

Solving this problem in a cloud native manner enables:

- **Portability:** Helm-based deployment to any Kubernetes cluster, enabling registry operation on-premises, in the cloud, or at the edge.
- **Scalability:** Containerized components that scale independently with cluster resources.
- **Integration:** Native compatibility with cloud native tooling (Kubernetes, Docker Compose, CI/CD pipelines, and the broader CNCF ecosystem).
- **Standardization:** Alignment with the Model Context Protocol (MCP) and emerging agentic AI standards such as the Agent-to-Agent (A2A) protocol.




---

### Usability

**Who are the target users of this project?**

agentregistry serves two primary personas:

**Operators / Platform Teams**
- Import, enrich, validate, and curate AI artifacts from any registry.
- Build and manage approved artifact collections (curated catalogs).
- Enforce governance, apply scoring/validation pipelines, and maintain audit control.
- Deploy artifacts to teams through centralized, governed channels.

**Developers / Application Builders**
- Discover pre-approved MCP servers, agents, and skills from the registry.
- Pull and run agentic artifacts with confidence in their trustworthiness.
- Push custom artifacts (MCP servers, skills, agents) to the registry for team-wide use.
- Integrate artifacts directly into AI-powered IDEs (Claude Code, Cursor, VS Code).

**How should the target personas interact with your project?**

- **CLI (`arctl`):** The primary interaction surface for both operators and developers. Supports `mcp list`, `configure`, and artifact push/pull operations. Installed via a shell script or binary download.
- **Web UI:** Accessible at `http://localhost:12121` (or a cluster-exposed endpoint), providing a visual interface for browsing, managing, and publishing artifacts.
- **Helm Chart / Kubernetes:** Operators deploy and manage the registry server on Kubernetes clusters using the published OCI Helm chart at `ghcr.io/agentregistry-dev/charts/agentregistry`.
- **Docker Compose:** Developers can run a local registry instance using Docker Compose for development and testing.

---

### Design

**Explain the design principles and best practices the project is following.**

agentregistry is designed around the following principles:

1. **Centralization with portability:** A single registry server acts as the source of truth for artifacts, yet is deployable anywhere via container images and Helm.
2. **Governance first:** All artifacts are subject to operator-controlled curation, approval, and access control before reaching developers.
3. **Data enrichment by default:** Ingested artifacts are automatically validated and scored to provide operators with trustworthiness insights.
4. **Protocol alignment:** The project aligns with the Model Context Protocol (MCP) specification, which is rapidly becoming the de facto standard for AI tool interoperability.
5. **Separation of concerns:** The registry server, CLI, and web UI are distinct components with well-defined interfaces.
6. **Open source and vendor-neutral:** Licensed under Apache 2.0; no vendor lock-in for registry operations or artifact formats.

**Outline or link to the project's architecture requirements.**

See [`DEVELOPMENT.md`](https://github.com/agentregistry-dev/agentregistry/blob/main/DEVELOPMENT.md) for detailed architecture information.

At a high level, the project comprises:

| Component | Description |
|---|---|
| **Registry Server** | Core Go service exposing the REST API for artifact management. Stores metadata in PostgreSQL + pgvector for semantic search. |
| **PostgreSQL + pgvector** | Persistent storage backend. pgvector enables embedding-based discovery and search. |
| **arctl CLI** | Go-based command-line interface. Communicates with the registry server over HTTP. Manages the server daemon lifecycle on first run. |
| **Web UI** | TypeScript/React frontend served by the registry server. Accessible at port 12121. |
| **Agent Gateway** | Optional integration with [agentgateway](https://github.com/agentgateway/agentgateway) as a reverse proxy providing a unified MCP endpoint for all deployed servers. |

**Architecture overview (Operator scenario):**

```
[External Registries / Artifact Sources]
           │  import
           ▼
   ┌──────────────────┐      enrich / validate / score
   │  Registry Server │◄─────────────────────────────
   │  (Go + REST API) │
   └────────┬─────────┘
            │  publish (curated)
            ▼
   ┌──────────────────┐
   │  PostgreSQL +    │  metadata + vector embeddings
   │  pgvector        │
   └──────────────────┘
            │
            ▼
   ┌──────────────────┐
   │  Web UI / arctl  │  operators and developers
   └──────────────────┘
```

**Architecture overview (Developer scenario):**

```
[Developer: Claude Code / Cursor / VS Code]
           │  arctl configure
           ▼
   ┌──────────────────┐
   │  Agent Gateway   │  unified MCP endpoint
   └────────┬─────────┘
            │  proxy
            ▼
   ┌──────────────────┐
   │  Deployed MCP    │  pulled from registry
   │  Servers/Skills  │
   └──────────────────┘
```

**Describe how this project integrates with other projects in a production environment.**

agentregistry integrates with:

- **agentgateway (Linux Foundation):** Acts as the data plane, providing a single MCP endpoint for all deployed servers and enforcing policy and observability.
- **MCP SDK / Model Context Protocol:** Core protocol for tool and agent interoperability.
- **Kubernetes / Helm:** Deployment and lifecycle management.
- **PostgreSQL + pgvector:** Metadata persistence and semantic discovery.
- **Docker / OCI:** Container image format for artifact packaging and distribution.
- **CI/CD tooling:** `arctl` can be embedded in CI/CD pipelines for artifact publishing workflows.

**Describe the project's architecture requirements for PoC, Development, Test, and Production environments.**

| Environment | Configuration |
|---|---|
| **PoC / Local** | Docker Compose with bundled PostgreSQL/pgvector. Single node. `arctl mcp list` auto-starts daemon. |
| **Development** | Docker Compose or Kind (local Kubernetes). See `scripts/kind/README.md`. |
| **Test** | Kubernetes (Kind) with Helm chart and an external PostgreSQL/pgvector instance. |
| **Production** | Kubernetes cluster with Helm chart (`oci://ghcr.io/agentregistry-dev/helm/agentregistry`). Requires an external, HA PostgreSQL instance with pgvector extension. |

**Define any specific service dependencies the project relies on.**

- **PostgreSQL ≥ 16 with pgvector extension:** Required for all environments except local PoC (where it is bundled via Docker Compose). The pgvector extension is required for semantic search capabilities.
- **Kubernetes (production):** Required for Helm-based deployment.
- **Docker / container runtime:** Required for running the registry server and related services.

**Describe the project's High Availability (HA) requirements.**

The registry server is stateless; HA is achieved by running multiple replicas behind a load balancer in Kubernetes. PostgreSQL HA is the responsibility of the operator (e.g., using CloudNativePG or a managed cloud database service). [TODO: document specific HA topology and failover behavior]

**Describe the project's resource requirements (CPU, Memory, Network).**

[TODO: Provide baseline resource benchmarks for the registry server, Agent Gateway, and PostgreSQL components under representative workloads.]

**Describe how the project implements Identity and Access Management.**

[TODO: Document the current authentication and authorization model. Based on the repository, JWT-based authentication is used (see `config.jwtPrivateKey` in the Helm chart). Describe: (1) how tokens are issued and validated, (2) role definitions and RBAC model, (3) planned support for external identity providers (OIDC, OAuth2).]

The CLI currently requires a `config.jwtPrivateKey` to be set at deployment time. Access to the registry API is gated by JWT tokens.

**Describe how the project has addressed sovereignty.**

[TODO: Document data residency capabilities. Because agentregistry is self-hosted (no external SaaS dependency for core registry functions), operators retain full control over artifact metadata and deployed registry data within their own infrastructure.]

**Describe any compliance requirements addressed by the project.**

[TODO: Identify any regulatory or compliance frameworks (e.g., SOC 2, GDPR, FedRAMP) that the project is designed to support or that early adopters have raised.]

---

### Security

**Describe the project's secure design principles.**

agentregistry addresses key security concerns in the agentic AI ecosystem:

- **Curated artifact ingestion:** Operators control which MCP servers, agents, and skills are approved before developers can consume them, preventing arbitrary code execution from unvetted sources.
- **Scoring and validation:** Ingested artifacts are automatically scored and validated, enriching metadata with trustworthiness signals.
- **JWT-based API authentication:** The registry server requires authentication for write operations.
- **Audit trail:** The registry is designed to maintain end-to-end audit and control over artifact lifecycle. [TODO: Document audit log specifics.]

**Describe the project's vulnerability reporting process.**

[TODO: Add a `SECURITY.md` file to the repository (currently absent) that defines: (1) how to report security vulnerabilities (e.g., private GitHub Security Advisories or a dedicated email), (2) the response SLA, (3) the CVE disclosure process. This is required for CNCF Sandbox acceptance.]

**How does the project handle CVEs in its dependencies?**

[TODO: Describe the dependency scanning setup. The repository includes a `.golangci.yaml` linter configuration and GitHub Actions workflows. Document whether automated dependency scanning (e.g., Dependabot, `govulncheck`, or Snyk) is in place and how identified vulnerabilities are triaged and remediated.]

**How does the project incorporate Source Composition Analysis (SCA)?**

[TODO: Document the SCA tooling integrated into the CI/CD pipeline (GitHub Actions). At minimum, `govulncheck` for Go dependencies and a frontend dependency audit for the TypeScript UI should be described.]

**Describe the known failure modes and recovery approach.**

| Component | Failure Mode | Recovery |
|---|---|---|
| Registry Server | Process crash or pod restart | Kubernetes restarts the pod; stateless design means no data loss. |
| PostgreSQL | Database unavailability | Registry API returns errors; no writes committed during outage. Restart or failover to HA replica. |
| Agent Gateway | Proxy unavailability | AI IDE clients lose connectivity to MCP servers; restart the gateway pod. |

[TODO: Expand with additional failure scenarios and runbook links.]

---

## Open Items for Maintainer Review

The following items require input from the agentregistry maintainers before this GTR is considered complete:

1. **SECURITY.md:** Add a vulnerability reporting policy and response SLA to the repository.
2. **GOVERNANCE.md:** Document the project's governance model, maintainer roles, and decision-making process.
3. **Public Roadmap:** Publish a roadmap documenting the path from v0.1.x to a stable v1.0 and eventual CNCF Incubation readiness.
4. **IAM documentation:** Detail the JWT-based authentication model, planned RBAC roles, and future OIDC/OAuth2 integration plans.
5. **SCA / dependency scanning:** Document the automated tooling used to scan Go and TypeScript dependencies for vulnerabilities.
6. **Attribution / license notices:** Describe how license compliance is enforced for dependencies in built artifacts (container images, binaries).
7. **HA topology:** Document the recommended HA configuration for production PostgreSQL and the registry server.
8. **Resource benchmarks:** Provide baseline CPU/memory/network resource requirements for production sizing.
9. **Audit logging:** Document the audit trail capabilities for artifact lifecycle events.
10. **Failure mode runbooks:** Expand the known failure modes table with recovery procedures and links to operational documentation.

---

