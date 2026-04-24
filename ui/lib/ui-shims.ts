// UI compat shims for the v1alpha1 refactor.
//
// The `make gen-client` regen in commit ebdb837 renamed every symbol
// the UI consumes (listServersV0 → listMcpserversAllNamespaces,
// ServerResponse → MCPServer, etc.) and changed the type shape from
// the flat legacy `{server: {...}}` to the K8s-style
// `{apiVersion, kind, metadata, spec}`. Rather than rewrite every
// rendering component to read the new nested envelope, this module
// exposes:
//
//   - Adapter types that reconstruct the old flat `{server: {...}}`
//     shape from the new envelope.
//   - Alias list functions that default the path namespace and map
//     responses through the adapter.
//   - Old symbol names (ServerResponse / SkillResponse / etc.) as
//     exports so imports stay source-compatible.
//
// Longer-term the UI should speak the envelope directly. This shim
// lets the CI pipeline build while that migration happens separately.

import type {
  Agent,
  AgentSpec,
  Deployment,
  McpServer,
  McpServerSpec,
  Prompt,
  PromptSpec,
  Skill,
  SkillSpec,
} from "@/lib/api/types.gen"
import {
  applyDeployment as applyDeploymentRaw,
  applyMcpserver as applyMcpserverRaw,
  applyPrompt as applyPromptRaw,
  applySkill as applySkillRaw,
  listAgents as listAgentsRaw,
  listMcpservers as listMcpserversRaw,
  listPrompts as listPromptsRaw,
  listSkills as listSkillsRaw,
} from "@/lib/api/sdk.gen"

// Cross-namespace listing used to be its own endpoint
// (`/v0/{plural}` returning all namespaces). After the route flatten it
// merged into the namespaced list with a `?namespace=all` query
// sentinel; the shim still wants that semantic, so layer it on top of
// any caller-supplied query.
function withAllNamespaces<Q extends Record<string, unknown> | undefined>(query: Q): Q {
  return { namespace: "all", ...(query ?? {}) } as unknown as Q
}

// ----------------------------------------------------------------------------
// Old-shape wire types. Each mirrors the legacy `{server: {...}}` /
// `{skill: {...}}` wrapper the UI consumed before the regen.
// ----------------------------------------------------------------------------

type LegacyInner<Spec, Extras = object> = Spec & {
  name: string
  // namespace is always populated by the adapter from ObjectMeta.namespace,
  // but test/stories mocks construct LegacyInner directly without it.
  namespace?: string
  version: string
  title?: string
  // $schema is a legacy ServerJson-only field; tolerated on the inner type so
  // fixtures can pin a schema URL without widening McpServerSpec.
  $schema?: string
  _meta?: Record<string, any>
  publishedAt?: string
  updatedAt?: string
  status?: string
} & Extras

// Legacy responses had `_meta` at BOTH the outer level and the nested
// `.server`/`.skill`/etc. level — the outer copy typically held
// MCP registry "official"/"publisher-provided" flags while the inner
// copy held spec-authored metadata. Shim populates both from
// ObjectMeta.annotations.
export interface ServerResponse {
  server: LegacyInner<McpServerSpec>
  _meta?: Record<string, any>
}

export interface SkillResponse {
  skill: LegacyInner<SkillSpec>
  _meta?: Record<string, any>
}

export interface AgentResponse {
  agent: LegacyInner<AgentSpec>
  _meta?: Record<string, any>
}

export interface PromptResponse {
  prompt: LegacyInner<PromptSpec>
  _meta?: Record<string, any>
}

// ----------------------------------------------------------------------------
// Envelope → legacy-shape adapters.
// ----------------------------------------------------------------------------

// namespace is now optional in the regen'd ObjectMeta because the wire
// strips "default" — fall back to "default" so legacy renderers keep
// composing display IDs the same way.
function inner<Spec extends object>(
  meta: { name: string; namespace?: string; version?: string; annotations?: Record<string, string>; createdAt?: string },
  spec: Spec,
): LegacyInner<Spec> {
  return {
    ...spec,
    name: meta.name,
    namespace: meta.namespace ?? "default",
    version: meta.version ?? "",
    publishedAt: meta.createdAt,
    _meta: meta.annotations ?? {},
  } as LegacyInner<Spec>
}

export function toServerResponse(m: McpServer): ServerResponse {
  return { server: inner(m.metadata, m.spec), _meta: m.metadata.annotations ?? {} }
}

export function toSkillResponse(s: Skill): SkillResponse {
  return { skill: inner(s.metadata, s.spec), _meta: s.metadata.annotations ?? {} }
}

export function toAgentResponse(a: Agent): AgentResponse {
  return { agent: inner(a.metadata, a.spec), _meta: a.metadata.annotations ?? {} }
}

export function toPromptResponse(p: Prompt): PromptResponse {
  return { prompt: inner(p.metadata, p.spec), _meta: p.metadata.annotations ?? {} }
}

// ----------------------------------------------------------------------------
// List-function aliases. Each wraps the regen'd *AllNamespaces endpoint and
// maps items through the adapter.
//
// Legacy callers expect `{ query: { cursor, limit } }` + a response with a
// `metadata: { nextCursor }` field. The shim threads those through the new
// cursor-based list surface.
// ----------------------------------------------------------------------------

interface LegacyListOpts {
  throwOnError?: true
  query?: { cursor?: string; limit?: number }
}

interface LegacyListMetadata {
  nextCursor?: string
}

export async function listServersV0(opts?: LegacyListOpts): Promise<{
  data: { servers: ServerResponse[]; metadata: LegacyListMetadata }
}> {
  const { data } = await listMcpserversRaw({
    throwOnError: true,
    query: withAllNamespaces(opts?.query),
  })
  return {
    data: {
      servers: (data?.items ?? []).map(toServerResponse),
      metadata: { nextCursor: data?.nextCursor },
    },
  }
}

export async function listSkillsV0(opts?: LegacyListOpts): Promise<{
  data: { skills: SkillResponse[]; metadata: LegacyListMetadata }
}> {
  const { data } = await listSkillsRaw({
    throwOnError: true,
    query: withAllNamespaces(opts?.query),
  })
  return {
    data: {
      skills: (data?.items ?? []).map(toSkillResponse),
      metadata: { nextCursor: data?.nextCursor },
    },
  }
}

export async function listAgentsV0(opts?: LegacyListOpts): Promise<{
  data: { agents: AgentResponse[]; metadata: LegacyListMetadata }
}> {
  const { data } = await listAgentsRaw({
    throwOnError: true,
    query: withAllNamespaces(opts?.query),
  })
  return {
    data: {
      agents: (data?.items ?? []).map(toAgentResponse),
      metadata: { nextCursor: data?.nextCursor },
    },
  }
}

export async function listPromptsV0(opts?: LegacyListOpts): Promise<{
  data: { prompts: PromptResponse[]; metadata: LegacyListMetadata }
}> {
  const { data } = await listPromptsRaw({
    throwOnError: true,
    query: withAllNamespaces(opts?.query),
  })
  return {
    data: {
      prompts: (data?.items ?? []).map(toPromptResponse),
      metadata: { nextCursor: data?.nextCursor },
    },
  }
}

// ----------------------------------------------------------------------------
// Create-function shims. Legacy callers pass a flat `{name: "ns/name", version,
// description, ...spec}` JSON; the regen'd apply* endpoints take a K8s
// envelope plus `{namespace, name, version}` in the path. These helpers split
// `name` into namespace/name, wrap the spec in an envelope, and call apply.
// ----------------------------------------------------------------------------

export interface ServerJson extends McpServerSpec {
  $schema?: string
  name: string
  version: string
}

export interface SkillJson extends SkillSpec {
  name: string
  version: string
}

export interface PromptJson extends PromptSpec {
  name: string
  version: string
}

export interface AgentJson extends AgentSpec {
  name: string
  version: string
}

interface LegacyCreateOpts<Body> {
  throwOnError?: true
  body: Body
}

// splitName turns the legacy "namespace/name" identifier into
// the (namespace, name) pair the envelope expects. Names without a
// namespace fall back to "default".
function splitName(fullName: string): { namespace: string; name: string } {
  const idx = fullName.indexOf("/")
  if (idx < 0) return { namespace: "default", name: fullName }
  return { namespace: fullName.slice(0, idx), name: fullName.slice(idx + 1) }
}

function stripLegacy<T extends { name: string; version: string }>(body: T): object {
  const { name: _n, version: _v, ...rest } = body as T & { $schema?: string }
  delete (rest as { $schema?: string }).$schema
  return rest
}

export async function createServerV0(opts: LegacyCreateOpts<ServerJson>): Promise<{
  data: ServerResponse
}> {
  const { namespace, name } = splitName(opts.body.name)
  const spec = stripLegacy(opts.body) as McpServerSpec
  const { data } = await applyMcpserverRaw({
    throwOnError: true,
    path: { name, version: opts.body.version }, query: namespace !== "default" ? { namespace } : undefined,
    body: {
      apiVersion: "agentregistry.solo.io/v1alpha1",
      kind: "MCPServer",
      metadata: { namespace, name, version: opts.body.version },
      spec,
    },
  })
  return { data: toServerResponse(data as McpServer) }
}

export async function createSkillV0(opts: LegacyCreateOpts<SkillJson>): Promise<{
  data: SkillResponse
}> {
  const { namespace, name } = splitName(opts.body.name)
  const spec = stripLegacy(opts.body) as SkillSpec
  const { data } = await applySkillRaw({
    throwOnError: true,
    path: { name, version: opts.body.version }, query: namespace !== "default" ? { namespace } : undefined,
    body: {
      apiVersion: "agentregistry.solo.io/v1alpha1",
      kind: "Skill",
      metadata: { namespace, name, version: opts.body.version },
      spec,
    },
  })
  return { data: toSkillResponse(data as Skill) }
}

export async function createPromptV0(opts: LegacyCreateOpts<PromptJson>): Promise<{
  data: PromptResponse
}> {
  const { namespace, name } = splitName(opts.body.name)
  const spec = stripLegacy(opts.body) as PromptSpec
  const { data } = await applyPromptRaw({
    throwOnError: true,
    path: { name, version: opts.body.version }, query: namespace !== "default" ? { namespace } : undefined,
    body: {
      apiVersion: "agentregistry.solo.io/v1alpha1",
      kind: "Prompt",
      metadata: { namespace, name, version: opts.body.version },
      spec,
    },
  })
  return { data: toPromptResponse(data as Prompt) }
}

// ----------------------------------------------------------------------------
// deployServer: legacy imperative deploy endpoint replaced by declarative
// Deployment upsert. Legacy body fields: {serverName, version, env,
// preferRemote, providerId, resourceType}. Translate to a Deployment envelope.
// ----------------------------------------------------------------------------

export interface DeployServerBody {
  serverName: string
  version: string
  env?: Record<string, string>
  preferRemote?: boolean
  providerId: string
  resourceType?: "agent" | "mcp" | string
}

function resourceTypeToKind(rt?: string): string {
  switch (rt) {
    case "agent":
      return "Agent"
    case "mcp":
    case undefined:
    case "":
      return "MCPServer"
    default:
      return rt.charAt(0).toUpperCase() + rt.slice(1)
  }
}

export async function deployServer(opts: { throwOnError?: true; body: DeployServerBody }): Promise<{
  data: Deployment
}> {
  const { namespace, name } = splitName(opts.body.serverName)
  const kind = resourceTypeToKind(opts.body.resourceType)
  // Deployment name is derived from the (kind, target name) pair so that
  // multiple deployments of different resource types can coexist in a
  // namespace. Keep this stable so the legacy UI can find/update it.
  const deploymentName = `${name}-${kind.toLowerCase()}`
  const { data } = await applyDeploymentRaw({
    throwOnError: true,
    path: { name: deploymentName, version: opts.body.version }, query: namespace !== "default" ? { namespace } : undefined,
    body: {
      apiVersion: "agentregistry.solo.io/v1alpha1",
      kind: "Deployment",
      metadata: { namespace, name: deploymentName, version: opts.body.version },
      spec: {
        targetRef: { kind, name, namespace, version: opts.body.version },
        providerRef: { kind: "Provider", name: opts.body.providerId, namespace },
        env: opts.body.env,
        preferRemote: opts.body.preferRemote,
      },
    },
  })
  return { data: data as Deployment }
}
