# Extract `RemoteMCPServer` as a top-level v1alpha1 kind

> Status: **Approved** — team signed off; implementation in progress on
> `refactor/extract-remote-mcp-server`.
>
> Scope of this PR:
>
> 1. Split `MCPServer.Spec.Remotes` out into a new `RemoteMCPServer` kind.
> 2. Keep `AgentSpec.Remotes` removed. Remote agents are not supported today, so
>    this PR does **not** introduce a `RemoteAgent` kind.
> 3. Use `ResourceRef.Kind` discrimination so `AgentSpec.MCPServers` can point
>    at either `MCPServer` or `RemoteMCPServer`.

## 1. Problem

`MCPServer.Spec.Remotes []MCPTransport` overloads the kind. Today an
`MCPServer` is doing two unrelated jobs:

1. **Registry template** — catalog entry describing how to deploy an MCP server
   (packages, runtime hints, env/args).
2. **Pre-deployed endpoint pointer** — a URL to an already-running MCP server
   that the registry only needs to call.

Code symptoms of the overload:

- `TranslateMCPServer` had `useRemote` / `PreferRemote` branching.
- `MCPServerRunRequest.PreferRemote` existed only to disambiguate within a
  single overloaded resource.
- `MCPServer.Status` has nothing meaningful to say about a remote endpoint
  because the same kind also represents a non-deployed template.

Conceptually, a remote MCP server sits at the same level as a `Deployment`: it is
the directly invokable thing, not a deployable template. Splitting it into its
own kind lets each kind carry metadata and status semantics that fit.

## 2. New Kind

```go
type RemoteMCPServer struct {
    TypeMeta `json:",inline" yaml:",inline"`
    Metadata ObjectMeta          `json:"metadata" yaml:"metadata"`
    Spec     RemoteMCPServerSpec `json:"spec" yaml:"spec"`
    Status   Status              `json:"status,omitzero" yaml:"status,omitempty"`
}

type RemoteMCPServerSpec struct {
    Title       string       `json:"title,omitempty" yaml:"title,omitempty"`
    Description string       `json:"description,omitempty" yaml:"description,omitempty"`

    // Remote is the connection endpoint of the running MCP server.
    // Validator rejects empty Remote: Type and URL are both required.
    Remote MCPTransport `json:"remote,omitempty" yaml:"remote,omitempty"`
}
```

`MCPServerSpec.Remotes []MCPTransport` is removed in this PR. Existing manifests
with `remotes:` under an `MCPServer` must split into a sibling
`RemoteMCPServer` resource.

`AgentSpec.Remotes []AgentRemote` is also removed, but it is not replaced by a
new remote-agent kind. Remote agents have no supported runtime meaning today.

## 3. References

`AgentSpec.MCPServers []ResourceRef` accepts either kind, discriminated by
`ResourceRef.Kind`:

- `Kind: "MCPServer"` (default) — bundled template.
- `Kind: "RemoteMCPServer"` — already-running endpoint.

`Deployment.Spec.TargetRef` accepts:

- `Kind: "Agent"` / `Kind: "MCPServer"` — bundled, lifecycle-managed targets.
- `Kind: "RemoteMCPServer"` — register-only MCP endpoint.

There is intentionally no `RemoteAgent` target kind in this PR.

## 4. Provider Binding

`RemoteMCPServerSpec` stays provider-agnostic. Binding to a `Provider` lives on a
`Deployment` resource whose `TargetRef.Kind` is `RemoteMCPServer`.

The existing deployment coordinator and platform adapters dispatch on
`TargetRef.Kind`. Bundled targets keep container/process lifecycle. A
`RemoteMCPServer` target does thin pass-through registration, such as emitting a
`kagent.dev/v1alpha2.RemoteMCPServer` CR on Kubernetes.

Health probing of remote endpoints is deferred. This PR ships no
`Reachable` / `HandshakeOK` / `LastChecked` status keys.

## 5. Implementation Plan

- Add `pkg/api/v1alpha1/remotemcpserver.go` and validator/test coverage.
- Add `KindRemoteMCPServer` to `BuiltinKinds`, `scheme.go`, CRUD bindings, and
  `v1alpha1store.TableFor`.
- Add `v1alpha1.remote_mcp_servers` storage table.
- Remove `MCPServerSpec.Remotes`, `AgentSpec.Remotes`,
  `DeploymentSpec.PreferRemote`, and `MCPServerRunRequest.PreferRemote`.
- Update agent MCP-server ref validation and manifest resolution to allow
  `KindRemoteMCPServer`.
- Update deployment validation and platform adapters to accept
  `TargetRef.Kind == "RemoteMCPServer"`.
- Rename internal `platformtypes.RemoteMCPServer` to `RemoteMCPTarget` to avoid
  colliding with the new public kind and the kagent CRD.
- Update seed/importer logic to emit sibling `RemoteMCPServer` rows for upstream
  remotes and stamp `agentregistry.dev/related-mcpserver`.
- Regenerate OpenAPI and the UI TypeScript client.
- Add CLI/UI/e2e coverage for `RemoteMCPServer`.

## 6. Migration

One-shot startup migration:

1. For every `v1alpha1.mcp_servers` row whose spec contains non-empty
   `remotes`, insert one `v1alpha1.remote_mcp_servers` row per remote.
2. If the original MCP server has no packages, delete the source `mcp_servers`
   row and rewrite affected `Deployment.Spec.TargetRef.Kind` from `MCPServer` to
   `RemoteMCPServer`.
3. If the original MCP server has packages, strip `remotes` from the source row.
4. Strip unsupported `remotes` from existing agent rows without creating any new
   remote-agent resource.
5. Drop the obsolete `preferRemote` key from existing deployment specs.

## 7. Breaking Changes

- `MCPServerSpec.Remotes` removed.
- `AgentSpec.Remotes` removed.
- `DeploymentSpec.PreferRemote` and the CLI `--prefer-remote` flag removed.
- Internal DTO rename: `platformtypes.RemoteMCPServer` → `RemoteMCPTarget`.

## 8. Decisions

1. `RemoteMCPServer.Remote` is singular.
2. Empty `Remote` is rejected at write time; `Type` and `URL` are required.
3. Importer populates `metadata.annotations["agentregistry.dev/related-mcpserver"]`.
4. No probe/status condition semantics in this PR.
5. Remote agents are out of scope and unsupported; no `RemoteAgent` kind lands.
6. Enterprise authz only needs `KindRemoteMCPServer` mapped to the server
   artifact type.
