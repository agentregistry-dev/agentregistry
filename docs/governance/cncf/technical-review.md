# General Technical Review — Agentregistry / Sandbox

_This document provides a General Technical Review of the agentregistry project. This is a living document that demonstrates to the Technical Advisory Group (TAG) that the project satisfies the Engineering Principle requirements for moving levels. This document follows the template outlined in the [TOC subproject review](https://github.com/cncf/toc/blob/main/toc_subprojects/project-reviews-subproject/general-technical-questions.md)_


> **Project:** agentregistry
> **Project Version:** v0.3.2
> **Website:** https://aregistry.ai
> **Date Updated:** 2026-03-17
> **Template Version:** v1.0
> **Description:** Agentregistry gives platform teams and developers one place to manage the agentic infrastructure their applications depend on.

---

# Day 0 — Planning Phase (Sandbox)

This section covers the design and architecture of Agentregistry as a cloud native project applying for CNCF Sandbox status.

---

## Scope

**Describe the roadmap process, how scope is determined for mid to long term features, as well as how the roadmap maps back to current contributions and maintainer ladder.**

The roadmap can be found here: https://github.com/orgs/agentregistry-dev/projects/3

Work is tracked in GitHub issues, and then feature requests are prioritized into the `Ready` column when they are ready for an engineer to start working on them.

https://github.com/agentregistry-dev/community/blob/main/CONTRIBUTING.md outlines how individuals can contribute to the project.

**Describe the target persona or user(s) for the project.**

Agentregistry is designed for two primary personas, both of whom interact with the same registry but with distinct concerns:

- **Operators / Platform Engineering Teams:** Operators are responsible for the governance and lifecycle of AI infrastructure within an organization. They import AI artifacts from external sources, apply validation and scoring pipelines to assess trustworthiness, and publish curated, approved collections for developer consumption. Their primary concerns are control, auditability, and preventing unapproved or unvetted AI tools from reaching production systems.
- **Developers / Application Builders:** Developers are building AI-powered applications or workflows and need a trusted, discoverable source of MCP servers, agents, and skills. They consume artifacts that have been pre-approved by operators, integrate them into AI-powered IDEs (Claude Code, Cursor, VS Code), and may also publish their own custom artifacts back to the registry for team-wide sharing.

**Explain the primary use case for the project. What additional use cases are supported by the project?**

The primary use case is providing organizations with a centralized, trusted registry where MCP servers, AI agents, and skills can be published, discovered, and consumed with governance controls in place. Without agentregistry, teams have no standardized way to vet, curate, or distribute the AI tools their developers use, leading to fragmentation and risk. agentregistry solves this by functioning as the control plane for agentic infrastructure: operators import and curate artifacts, and developers pull only from the approved catalog.

A concrete example shown in the project documentation is publishing an Anthropic Skill to the registry and consuming it directly from Claude Code via `arctl configure claude-desktop`.

_Additional Supported Use Cases:_
- **MCP server aggregation via Agentgateway:** The registry integrates with [agentgateway](https://github.com/agentgateway/agentgateway) to expose all deployed MCP servers through a single unified endpoint. This allows AI IDE clients to connect once and access all available tools without per-server configuration.
- **IDE configuration generation:** `arctl configure` generates ready-to-use configuration files for Claude Desktop, Cursor, and VS Code, reducing the friction of connecting AI tools to a local or team registry.
- **Multi-environment artifact deployment:** Artifacts can be deployed to any target environment (local, cloud, Kubernetes) from a single registry, unifying AI infrastructure management across deployment targets.
- **Artifact enrichment and scoring:** The registry automatically validates and scores ingested artifacts, producing metadata that operators can use to assess safety, quality, and trustworthiness before approving artifacts for developer use.
- **Local development registry:** Developers can run a full registry locally via Docker Compose for testing and development workflows, with seed data automatically imported on first run.

**Explain which use cases have been identified as unsupported by the project.**

The following use cases are not supported, and are currently considered out of scope:
- **General-purpose OCI artifact or container image registry:** Agentregistry is purpose-built for agentic AI artifacts (MCP servers, agents, skills). It is not a replacement for OCI-compatible container registries such as Harbor or Docker Hub. Container image distribution is out of scope.
- **AI model registry or model storage:** Agentregistry does not store, version, or serve machine learning model weights or model files. It manages the metadata, configuration, and composition of agentic tools and skills, not the underlying models they invoke.
- **Runtime execution environment:** Agentregistry does not execute agents or MCP servers directly. Execution is handled by the target environment (e.g., the Agentgateway and the underlying runtime). The registry manages lifecycle metadata, not runtime orchestration.
- **Agent versioning (currently in progress):** Versioning for agents and skills is not yet implemented. Until released, use cases requiring immutable, versioned artifact references are not fully supported.

**Describe the intended types of organizations who would benefit from adopting this project. (i.e. financial services, any software manufacturer, organizations providing platform engineering services)?**

Agentregistry is broadly relevant to any organization building on AI-powered tooling at scale. The following organization types are most likely to benefit:

- **Software product companies and ISVs** building AI-native products who need to manage, curate, and distribute MCP servers, agents, and skills across development teams and customer environments. agentregistry provides the governance layer that prevents developers from pulling arbitrary, unvetted AI tools.
- **Platform engineering and internal developer platform (IDP) teams** who are responsible for providing a self-service, governed layer of AI tooling to internal developers. The operator/developer persona split maps directly to this function: platform teams act as operators, publishing curated AI artifact catalogs; developers self-serve from the approved catalog.
- **Financial services and regulated industries** where strict control over which software tools can be used in production is a compliance and audit requirement. agentregistry's curation model—where all artifacts must be explicitly approved before developer access—aligns well with change management and software supply chain controls required in regulated environments.
- **Government and public sector organizations** with similar supply chain governance requirements, where the ability to operate a fully self-hosted, air-gapped registry is critical.
- **AI/ML-focused consultancies and system integrators** who build and deliver agentic AI systems for enterprise clients. These organizations can use agentregistry to package and distribute their proprietary agents and skills to client environments in a governed, reproducible way.
- **Any organization standardizing on MCP** as its agentic AI infrastructure protocol, who needs a registry layer to manage the lifecycle of MCP servers across teams and environments.

---

## Usability

**How should the target personas interact with your project?**

Agentregistry provides two primary interaction surfaces — a CLI (`arctl`) and a Web UI — which map to different stages of the artifact lifecycle and to the two personas (Operators, Developers) described earlier.

_Operators interact primarily through the Web UI and the CLI for governance workflows._
1. **Import** — Pull AI artifacts (MCP servers, agents, skills) from external sources into the registry. This can be done via the Web UI using the purple `+ Add` button, selecting the artifact type (Agent, MCP Server, or Skill) and providing its metadata, name, description, version, and container image path or repository reference. The CLI `arctl apply -f skill.yaml` and `arctl apply -f mcp.yaml` commands are available for scripted or CI/CD-driven ingestion.
2. **Review and enrich** — Inspect automatically generated scores and validation metadata in the Web UI's artifact detail views (the Servers, Agents, and Skills views). Operators use this enriched metadata to make approval decisions.
3. **Curate and publish** — Selectively publish approved artifacts into a curated catalog that developers can access, maintaining end-to-end audit and control from the registry.
4. **Deploy to environments** — Use `arctl deploy` or the Web UI to promote approved artifacts to target environments (local Docker, Kubernetes clusters).

_Developers interact primarily through the CLI for day-to-day workflows._
1. **Install** — Install the `arctl` CLI via the provided shell script or by downloading a binary directly from the GitHub releases page:
   ```
   curl -fsSL https://raw.githubusercontent.com/agentregistry-dev/agentregistry/main/scripts/get-arctl | bash
   ```
2. **Discover** — Run `arctl daemon start` first to start the local registry daemon, then use `arctl get mcps` or `arctl list` to browse available artifacts from the registry.
3. **Configure IDEs** — Generate ready-to-use configuration files for AI-powered IDEs with a single command:
   - `arctl configure claude-desktop`
   - `arctl configure cursor`
   - `arctl configure vscode`
   These commands write the appropriate MCP configuration so the IDE routes tool calls through the agentgateway to the deployed servers.
4. **Create and publish** — Scaffold new agents, skills, or MCP servers using `arctl init agent`, `arctl init skill`, or `arctl init mcp`, then publish them back to the registry using `arctl apply -f <resource>.yaml`.
5. **Run and deploy** — Use `arctl run` to run an artifact locally, and `arctl deploy` to promote it to a target environment. The `arctl show` command retrieves full artifact details from the registry.

**Describe the user experience (UX) and user interface (UI) of the project.**

_This is described as part of the above answer_

**Describe how this project integrates with other projects in a production environment.**

In production, agentregistry acts as the **control plane** for agentic AI infrastructure — it manages the catalog, governance, and configuration of AI artifacts — while complementary projects handle execution, traffic routing, and deployment. The key integrations are:
- **agentgateway (Linux Foundation):** The most significant integration. Agentgateway is a reverse proxy purpose-built for AI traffic that provides a single, unified MCP endpoint for all deployed servers. In a production deployment, agentregistry and agentgateway work as a pair, where agentregistry holds the catalog of approved artifacts, while agentgateway receives mCP traffic from AI IDE clients (ie Claude Desktop, Cursor, VS Code) and droutes tools calls to the appropriate backend MCP server.
- **Kubernetes / Helm:** In production, agentregistry is deployed to a Kubernetes cluster using the published OCI Helm chart (`oci://ghcr.io/agentregistry-dev/agentregistry/charts/agentregistry`). It integrates with standard Kubernetes primitives: Deployments, Services, ConfigMaps, and Secrets (for the JWT private key and database credentials). The Kubernetes Gateway API (`gateway.networking.k8s.io`) is used for agentgateway routing configuration.
- **PostgreSQL with pgvector:** agentregistry requires PostgreSQL with the pgvector extension as its persistent storage backend. In production, this may be an externally managed PostgreSQL instance. The pgvector extension enables semantic/embedding-based search across the artifact catalog.
- **Container registries:** agentregistry integrates with whatever container registry an organization already uses, with no lock-in to a specific image storage backend.
- **AI-powered IDEs (Claude Desktop, Cursor, VS Code):** agentregistry integrates with AI IDEs not as a runtime dependency, but as a configuration provider. The `arctl configure` command writes MCP configuration files to the developer's local filesystem in the format expected by each IDE. Once configured, the IDE connects directly to the agentgateway; agentregistry is not in the request path at runtime.
- **Model Context Protocol (MCP):** agentregistry is built around MCP as the core protocol for tool and agent interoperability. MCP servers are the primary artifact type managed by the registry. Compatibility with the MCP specification is foundational to the project's design, and the registry is expected to track and align with MCP specification evolution over time.

---

## Design

**Explain the design principles and best practices the project is following.**
1. **Centralization with portability:** A single registry server acts as the source of truth for artifacts, yet is deployable anywhere via container images and Helm.
2. **Governance first:** All artifacts are subject to operator-controlled curation, approval, and access control before reaching developers.
3. **Data enrichment by default:** Ingested artifacts are automatically validated and scored to provide operators with trustworthiness insights.
4. **Protocol alignment:** The project aligns with the Model Context Protocol (MCP) specification, which is rapidly becoming the de facto standard for AI tool interoperability.
5. **Separation of concerns:** The registry server, CLI, and web UI are distinct components with well-defined interfaces.
6. **Open source and vendor-neutral:** Licensed under Apache 2.0; no vendor lock-in for registry operations or artifact formats.

**Outline or link to the project's architecture requirements.**
See [`DEVELOPMENT.md`](https://github.com/agentregistry-dev/agentregistry/blob/main/DEVELOPMENT.md) for detailed architecture information, specifically the `Architecture Overview` section.

**Describe how this project integrates with other projects in a production environment.**
- **agentgateway (Linux Foundation):** Acts as the data plane, providing a single MCP endpoint for all deployed servers and enforcing policy and observability.
- **MCP SDK / Model Context Protocol:** Core protocol for tool and agent interoperability.
- **Kubernetes / Helm:** Deployment and lifecycle management.
- **PostgreSQL + pgvector:** Metadata persistence and semantic discovery.
- **Docker / OCI:** Container image format for artifact packaging and distribution.
- **CI/CD tooling:** `arctl` can be embedded in CI/CD pipelines for artifact publishing workflows.

**Describe the project's architecture requirements for PoC, Development, Test, and Production environments.**
| Environment | Configuration |
|---|---|
| **PoC / Local** | Docker Compose with bundled PostgreSQL/pgvector. Single node. Daemon lifecycle is managed explicitly with `arctl daemon start` / `arctl daemon stop`. |
| **Development** | Docker Compose or Kind (local Kubernetes). See `scripts/kind/README.md`. |
| **Test** | Kubernetes (Kind) with Helm chart and an external PostgreSQL/pgvector instance. |
| **Production** | Kubernetes cluster with Helm chart (`oci://ghcr.io/agentregistry-dev/agentregistry/charts/agentregistry`). Requires an external, HA PostgreSQL instance with pgvector extension. |

**Define any specific service dependencies the project relies on.**
- **PostgreSQL ≥ 16 with pgvector extension:** Required for all environments except local PoC (where it is bundled via Docker Compose). The pgvector extension is required for semantic search capabilities.
- **Kubernetes (production):** Required for Helm-based deployment.
- **Docker / container runtime:** Required for running the registry server and related services.

**Describe the project's High Availability (HA) requirements.**
The registry server is stateless; HA is achieved by running multiple replicas behind a load balancer in Kubernetes. PostgreSQL HA is the responsibility of the operator (e.g., using CloudNativePG or a managed cloud database service). 

**Describe how the project has addressed sovereignty.**
Because agentregistry is self-hosted (no external SaaS dependency for core registry functions), operators retain full control over artifact metadata and deployed registry data within their own infrastructure.

**Describe any compliance requirements addressed by the project.**
No regulatory or compliance frameworks are currently supported. 

**Describe the project’s release processes, including major, minor and patch releases.**

agentregistry follows semantic versioning (https://semver.org/):
- Major Releases (x.0.0): Breaking API changes, major new features
- Minor Releases (0.x.0): New features, non-breaking changes.
- Patch Releases (0.0.x): Bug fixes, security patches

---

## Installation

**Describe how the project is installed and initialized, e.g. a minimal install with a few lines of code or does it require more complex integration and configuration?**
- _Local install with Docker_: Follow the steps at https://github.com/agentregistry-dev/agentregistry/blob/main/README.md#-local-development 
- _Kubernetes install with Helm_: Follow the steps at https://github.com/agentregistry-dev/agentregistry/blob/main/README.md#%EF%B8%8F-kubernetes 


**How does an adopter test and validate the installation?**

agentregistry does not currently ship a dedicated installation verification command (e.g., `arctl check` or a healthcheck subcommand). Validation is performed by combining CLI output and the Web UI.

---

## Security

**Please provide a link to the project’s cloud native security self assessment.**
[Security self-assessment](./security-self-assessment.md)

**How are you satisfying the tenets of cloud native security projects?**
Agentregistry applies cloud native security principles across its architecture, deployment model, and development practices. The project provides secure defaults out of the box while allowing operators to tune security controls for their environment.

**Describe how each of the cloud native principles apply to your project.**
- **Defense in Depth:** agentregistry employs multiple independent layers of security controls. API authentication is handled via JWT tokens, with support for external identity providers through OIDC. Authorization is enforced per-request through a dedicated `AuthzProvider`. At the infrastructure level, Kubernetes pod security contexts enforce non-root execution, read-only root filesystems, dropped Linux capabilities, and a RuntimeDefault seccomp profile. Database connections default to SSL mode `require`, encrypting data in transit between the registry server and PostgreSQL.
- **Least Privilege:** The Kubernetes RBAC configuration grants only the permissions necessary for the registry's core function of managing MCP server deployments.
- **Zero Trust:** Every API request is subject to authentication and authorization checks.
- **Secure Defaults:** The Helm chart ships with security-hardened defaults that require no additional configuration.
- **Separation of Concerns:**  The project is architected as distinct components with well-defined interfaces. The database is only accessed through the dedicated database layer. Authentication and authorization are handled through clearly defined interfaces.
- **Transparency:** Fully open-source under the Apache 2.0 license.

**How do you recommend users alter security defaults in order to "loosen" the security of the project?**
Operators who need to relax security controls for specific environments can use the [Helm API](https://github.com/agentregistry-dev/agentregistry/blob/main/charts/agentregistry/values.yaml).



**Describe the frameworks, practices, and procedures the project uses to maintain the basic health and security of the project.**
1. **Code Review**: All PRs require maintainer review before merge
2. **Automated Testing**: Unit, integration, and E2E tests in CI/CD
3. **Dependency Management**: Go modules with version pinning. Regular dependency updates
4. **Security Policy**: [SECURITY.md](/SECURITY.md) with responsible disclosure process

**Describe how the project has evaluated which features will be a security risk to users if they are not maintained by the project.**

The following features have been identified as carrying security risk if not actively maintained:

1. **JWT private key management** — The signing key is set statically at deploy time (`config.jwtPrivateKey` or via `existingSecret`). There is no built-in key rotation mechanism. If the key is compromised, all issued tokens are at risk until the key is manually rotated.
2. **Public action allowlist** — The current authorization implementation (`pkg/registry/auth/authz.go`) includes a temporary allowlist that permits `read`, `publish`, `delete`, and `deploy` actions without authentication. This is documented in the code as a development convenience and is flagged for removal before production hardening.
3. **Dependency vulnerability scanning** — The project does not currently have automated dependency scanning (e.g., Dependabot, Renovate, `govulncheck`, Trivy) integrated into CI. Vulnerabilities in transitive dependencies may go undetected without manual triage.
4. **Artifact signing and provenance** — Released container images and Helm charts are not signed (no cosign/sigstore integration) and no SBOM or provenance attestation is generated. Users cannot cryptographically verify the integrity of published artifacts.


**Explain the least minimal privileges required by the project and reasons for additional privileges.**

Operators can set `rbac.watchedNamespaces` in the Helm values to restrict the registry to specific namespaces, switching from a ClusterRole to per-namespace Roles. This limits the blast radius to only the namespaces the registry is authorized to manage.

**Describe how the project is handling certificate rotation and mitigates any issues with certificates.**

- **JWT Token Signing:** Tokens are signed with a private key that is configured at deploy time. There is no automated key rotation mechanism. Operators must manually update the signing key and restart the registry server to rotate keys.
- **Database TLS:** The PostgreSQL connection defaults to SSL mode `require`. Certificate management for the database connection is delegated to the operator's PostgreSQL infrastructure.

**Describe how the project is following and implementing secure software supply chain best practices.**
1. **Reproducible builds** — Container images are built via multi-stage Dockerfiles with pinned base image versions and deterministic Go module dependencies (`go.sum`).
2. **Multi-platform support** — Images are built for linux/amd64 and linux/arm64 using Docker Buildx, ensuring consistent builds across architectures.
3. **Checksums** — Helm chart releases include a `checksums.txt` file alongside the packaged charts and CLI binaries in the GitHub Release.
4. **Pinned CI dependencies** — GitHub Actions workflows use pinned versions for all actions (e.g., `actions/checkout@v4`, `actions/setup-go@v6`, `golangci/golangci-lint-action@v7`).
5. **Go module integrity** — Dependencies are verified against `go.sum` checksums and the Go module proxy/checksum database.
6. **Minimal permissions in CI** — Release workflows request only the permissions needed (`contents: read/write`, `packages: write`), following the principle of least privilege for GitHub Actions tokens.

The following gaps have been identified and are on the projects roadmap:
| Gap | Description | Planned Mitigation |
|---|---|---|
| Image signing | Container images are not signed with cosign/sigstore | Integrate cosign signing into the release workflow |
| SBOM generation | No Software Bill of Materials is published with releases | Add SBOM generation (e.g., syft) to the release pipeline |
| Provenance attestation | No SLSA provenance attestation for build artifacts | Integrate SLSA provenance generation |
| Automated vulnerability scanning | No govulncheck, Trivy, or Dependabot in CI | Add govulncheck for Go and npm audit for frontend dependencies |
| Dependency update automation | No Dependabot or Renovate configuration | Add automated dependency update tooling |
