# agentregistry Security Self-Assessment

This document provides a self-assessment of the agentregistry project following the guidelines outlined by the [CNCF TAG Security and Compliance group](https://tag-security.cncf.io/community/assessments/guide/self-assessment/#self-assessment). The purpose is to evaluate agentregistry's current security posture and alignment with best practices, ensuring that it is suitable for adoption at a CNCF Sandbox level.

## Table of Contents

- [Metadata](#metadata)
  - [Version history](#version-history)
  - [Security Links](#security-links)
- [Overview](#overview)
  - [Background](#background)
  - [Actors](#actors)
  - [Actions](#actions)
  - [Goals](#goals)
  - [Non-goals](#non-goals)
- [Self-Assessment Use](#self-assessment-use)
- [Security Functions and Features](#security-functions-and-features)
  - [Critical](#critical)
  - [Security Relevant](#security-relevant)
- [Project Compliance](#project-compliance)
  - [Future State](#future-state)
- [Secure Development Practices](#secure-development-practices)
  - [Development Pipeline](#development-pipeline)
  - [Communication Channels](#communication-channels)
  - [Ecosystem](#ecosystem)
- [Security Issue Resolution](#security-issue-resolution)
  - [Responsible Disclosure Process](#responsible-disclosure-process)
  - [Incident Response](#incident-response)
- [Appendix](#appendix)
  - [Known Issues Over Time](#known-issues-over-time)
  - [Open SSF Best Practices](#open-ssf-best-practices)
  - [Case Studies](#case-studies)
  - [Related Projects / Vendors](#related-projects--vendors)

## Metadata

### Version history

|   |  |
| - | - |
| March 17, 2026 | Initial Draft _(Sam Heilbron)_ |

### Security Links


|   |  |
| - | - |
| Software | [agentregistry Repository](https://github.com/agentregistry-dev/agentregistry) |
| Security Policy | [SECURITY.md](https://github.com/agentregistry-dev/agentregistry/blob/main/SECURITY.md) |
| Security Provider | No. agentregistry is a registry for agentic infrastructure; it is not a security provider. It provides governance and curation controls but should not be considered a security product. |
| Languages | Go, TypeScript/JavaScript |
| Security Insights | See [Project Compliance > Future State](#future-state) |
| Security File | See [Project Compliance > Future State](#future-state) |
| Cosign pub-key | See [Project Compliance > Future State](#future-state) |

## Overview

agentregistry is a centralized registry for securely curating, discovering, deploying, and managing agentic infrastructure including MCP (Model Context Protocol) servers, agents, and skills. It gives platform teams and developers one place to manage the agentic infrastructure their applications depend on.

### Background

The rapid growth of AI agents, MCP servers, and skills has created a fragmented ecosystem with no standardized way to discover, curate, validate, or govern agentic infrastructure. Organizations face challenges such as no centralized source of truth for AI artifacts, lack of governance controls over which AI tools are approved for company-wide use, difficulty deploying and managing AI artifacts consistently across multiple environments, and absence of metadata enrichment, scoring, or validation pipelines for agentic components. agentregistry addresses these gaps by providing a centralized, secure registry where teams can publish, discover, curate, and deploy AI artifacts with confidence.

### Actors

**Registry Server**: The core Go service exposing the REST API for artifact management. Stores metadata in PostgreSQL with pgvector for semantic search. Handles authentication, authorization, artifact lifecycle, and deployment orchestration.

**CLI (arctl)**: A Go-based command-line interface that communicates with the registry server over HTTP. Supports artifact discovery, publishing, deployment, and configuration of AI-powered IDEs. Manages the server daemon lifecycle on first run for local development.

**Web UI**: A TypeScript/React (Next.js 14) frontend served by the registry server. Provides a visual interface for browsing, managing, and publishing artifacts. Accessible at port 12121.

**Agentgateway**: An optional integration with [agentgateway](https://github.com/agentgateway/agentgateway) (Linux Foundation) that acts as a reverse proxy providing a unified MCP endpoint for all deployed servers and enforcing policy and observability.

**PostgreSQL + pgvector**: The persistent storage backend for artifact metadata and vector embeddings that enable semantic discovery and search.

### Actions

**Artifact Ingestion and Curation**: Operators import MCP servers, agents, and skills from external registries or sources. Ingested artifacts are automatically validated and scored to provide trustworthiness signals. Operators control which artifacts are approved before developers can consume them.

**Artifact Discovery and Consumption**: Developers discover pre-approved artifacts through the CLI or web UI. Artifacts can be pulled, configured, and integrated directly into AI-powered IDEs (Claude Code, Cursor, VS Code).

**Deployment to Kubernetes**: The registry server deploys MCP servers and agents to Kubernetes clusters using the kagent.dev CRDs. This involves creating and managing deployments, services, secrets, and configmaps. RBAC permissions are enforced through Kubernetes ClusterRoles or namespace-scoped Roles.

**API Authentication and Authorization**: The registry server authenticates API requests using JWT tokens signed with Ed25519 cryptography. Authorization is enforced per-request through a dedicated AuthzProvider interface with resource-pattern matching and action-based permissions (read, publish, edit, delete, deploy).

### Goals

- **Centralized Trusted Registry**: Provide a single source of truth for AI artifacts (MCP servers, agents, skills) that organizations can trust and govern.
- **Governance and Curation**: Enable operators to control which artifacts are approved, scored, validated, and available to developers before consumption.
- **Cloud-Native Deployment**: Deliver a Kubernetes-native deployment model via Helm charts with secure defaults, enabling registry operation on-premises, in the cloud, or at the edge.
- **Secure Artifact Lifecycle**: Maintain end-to-end audit and control over artifact ingestion, curation, publishing, and deployment.

### Non-goals

- **Direct Cluster Administration**: agentregistry does not replace Kubernetes RBAC or cluster security policies; it operates within existing security boundaries.
- **LLM Model Hosting**: agentregistry does not host or provide LLM models; it manages the infrastructure artifacts (MCP servers, agents, skills) that interact with models.
- **Runtime Security Enforcement**: agentregistry is a registry and deployment tool, not a runtime security agent. Runtime policy enforcement is delegated to components like the agentgateway, service meshes, or Kubernetes network policies.

## Self-Assessment Use

This self-assessment is created by the agentregistry team to perform an internal analysis of the project's security. It is not intended to provide a security audit of agentregistry, or function as an independent assessment or attestation of agentregistry's security health.

This document serves to provide agentregistry users with an initial understanding of agentregistry's security, where to find existing security documentation, agentregistry plans for security, and general overview of agentregistry security practices, both for development of agentregistry as well as security of agentregistry.

This document provides the CNCF TAG-Security with an initial understanding of agentregistry to assist in a joint-assessment, necessary for projects under incubation. Taken together, this document and the joint-assessment serve as a cornerstone for if and when agentregistry seeks graduation and is preparing for a security audit.

## Security Functions and Features

### Critical

- **JWT Authentication with Ed25519**: The registry server authenticates API requests using JWT tokens signed with Ed25519 cryptography. Tokens have a 5-minute expiration window to limit the blast radius of token compromise. The signing key is configured at deploy time via the `config.jwtPrivateKey` Helm value or a Kubernetes Secret referenced by `config.existingSecret`.

- **OIDC Integration**: The registry supports external identity federation through OpenID Connect (OIDC), allowing organizations to integrate their existing identity providers (GitHub OIDC, generic OIDC). Role-based permissions can be mapped from OIDC claims.

- **Kubernetes RBAC**: The Helm chart deploys a ClusterRole (or namespace-scoped Roles when `rbac.watchedNamespaces` is configured) that grants only the permissions necessary for the registry's core function of managing MCP server deployments. RBAC is enabled by default.

- **Pod Security Context**: All containers run with hardened security defaults: non-root user (UID/GID 1001), read-only root filesystem, all Linux capabilities dropped, privilege escalation disabled, and RuntimeDefault seccomp profile. These are enabled by default in the Helm chart.

- **Secret Management**: Integrates with Kubernetes secret management for storing sensitive data like JWT signing keys, database credentials, and deployment configuration. Supports external secret references (`existingSecret`, `global.existingSecret`) for integration with secret management tools (e.g., External Secrets Operator, Vault).

### Security Relevant

- **Database Encryption in Transit**: PostgreSQL connections default to SSL mode `require`, encrypting data in transit between the registry server and the database.

- **Authorization with Resource Pattern Matching**: The AuthzProvider interface enforces per-request authorization checks against the caller's permission set, supporting exact match, prefix match (with wildcard), and global resource patterns.

- **Artifact Scoring and Validation**: Ingested artifacts are automatically scored and validated using the OSSF Scorecard library, enriching metadata with trustworthiness and dependency health signals.

- **Container Image Security**: Production images are built using multi-stage Docker builds, with Alpine Linux used for builder stages and `ubuntu:22.04` as the final runtime base image to reduce the attack surface. Go binaries are stripped of debug symbols (`-s -w` flags). Images are published to `ghcr.io` with multi-platform support (linux/amd64, linux/arm64).

- **Namespace Denylist**: A namespace blocking mechanism prevents abuse by denying operations on restricted namespaces.

## Project Compliance

- **Apache 2.0 License**: The project is licensed under Apache 2.0, ensuring open source compliance and clear licensing terms.
- **Kubernetes Security Standards**: Follows Kubernetes security best practices including Pod Security Standards (restricted profile equivalent), RBAC with least-privilege scoping, and support for namespace isolation.
- **Container Security**: Adheres to container security best practices with minimal base images, non-root execution, read-only root filesystems, and all capabilities dropped by default.

### Future State

In the future, agentregistry intends to build and maintain compliance with several industry standards and frameworks:

**Supply Chain Levels for Software Artifacts (SLSA)**:
- All release artifacts to include signed provenance attestations with cryptographic verification
- Build process isolation and non-falsifiable provenance
- Both container images and release binaries to have complete SLSA provenance chains

**Container Security Standards**:
- All container images signed with Cosign using keyless signing
- Software Bill of Materials (SBOM) generation for all releases
- Multi-architecture container builds with attestation

**Automated Vulnerability Scanning**:
- Integration of `govulncheck` for Go dependencies and `npm audit` for frontend dependencies into the CI/CD pipeline
- Automated dependency update tooling (Dependabot or Renovate)
- Container image scanning with Trivy

## Secure Development Practices

### Development Pipeline

- **Code Reviews**: All code changes require review before merging to the main branch. GitHub merge groups are configured to ensure CI passes before merge. Reviews focus on functionality, security implications, and adherence to coding standards.
- **Automated Testing**: Comprehensive test suite including unit tests, integration tests, and Helm chart tests. Tests are automatically run on all pull requests via GitHub Actions and must pass before merging.
- **Static Code Analysis**: golangci-lint v2.8.0 runs on every push and pull request with a comprehensive linter configuration including security-relevant analyzers (staticcheck, govet, depguard with restricted import paths). Frontend code is linted via ESLint.
- **Dependency Management**: Go dependencies are tracked via `go.mod` and `go.sum` with checksum verification against the Go module proxy and checksum database. Frontend dependencies are managed via npm with `package-lock.json`.
- **Code Generation Verification**: A dedicated CI workflow (`verify.yml`) ensures that generated code is up to date and has not been manually modified, preventing drift between generated and committed code.
- **Release Integrity**: Release artifacts (CLI binaries, Helm charts) include checksums published alongside the GitHub Release. Multi-platform Docker images are built using Docker Buildx with pinned action versions.

### Communication Channels

|   |  |
| - | - |
| Documentation | https://aregistry.ai |
| Contributing | https://github.com/agentregistry-dev/agentregistry/blob/main/CONTRIBUTING.md |
| Discord | https://discord.gg/HTYNjF2y2t |
|   |  |

### Ecosystem

agentregistry operates within the cloud-native ecosystem as a Kubernetes-native application. It integrates with other technologies in this ecosystem:

- **Kubernetes**: Native integration with Kubernetes APIs, RBAC, and resource management for deploying and managing MCP servers and agents.
- **Helm**: Deployment and lifecycle management through OCI Helm charts published to `ghcr.io`.
- **agentgateway (Linux Foundation)**: Acts as the data plane, providing a single MCP endpoint for all deployed servers and enforcing policy and observability.
- **MCP Ecosystem**: Core alignment with the Model Context Protocol (MCP) specification for AI tool interoperability, supporting MCP servers, agents, and skills as first-class artifacts.
- **PostgreSQL + pgvector**: Metadata persistence and embedding-based semantic discovery.
- **Docker / OCI**: Container image format for artifact packaging and distribution.
- **OSSF Scorecard**: Integrated for dependency health evaluation of ingested artifacts.
- **CI/CD Tooling**: The `arctl` CLI can be embedded in CI/CD pipelines for artifact publishing workflows.

## Security Issue Resolution

### Responsible Disclosure Process

See the [cve](https://github.com/agentregistry-dev/community/blob/main/CVE.md) document.

### Incident Response

See the [incident response](https://github.com/agentregistry-dev/community/blob/main/SECURITY_RESPONSE.md) document.

## Appendix

### Known Issues Over Time

As of the time of this assessment, no critical security vulnerabilities have been publicly reported or discovered in agentregistry. The following areas have been identified as requiring hardening before production use:
- JWT signing key rotation is not automated; operators must manually rotate keys.
- No automated dependency vulnerability scanning (govulncheck, Trivy, Dependabot) is currently integrated into CI.

### Open SSF Best Practices

agentregistry is working toward OpenSSF Best Practices certification. The project incorporates the OSSF Scorecard library for evaluating the dependency health of ingested artifacts.

### Case Studies

No case studies are available at this time. The project is at an early stage (v0.3.2) and is applying for CNCF Sandbox status.

### Related Projects / Vendors

- **kagent**: A Kubernetes-native AI agent platform. agentregistry uses kagent's CRDs (`kagent.dev` API group) for deploying agents and MCP servers to Kubernetes clusters.
- **agentgateway (agentgateway)**: A Linux Foundation project that acts as a reverse proxy providing a unified MCP endpoint. agentregistry integrates with agentgateway as an optional data plane component.
- **MCP Registries**: While other MCP server registries exist, agentregistry differentiates through its governance-first approach with operator-controlled curation, scoring, and deployment lifecycle management.
