// Flat-shape adapter over the v1alpha1 Deployment type.
//
// The API migrated from imperative per-deployment CRUD (flat fields like
// id / serverName / resourceType / deployedAt / status string) to the
// K8s-style `{apiVersion, kind, metadata, spec, status}` envelope. The
// UI components weren't migrated at the same time, so this adapter
// bridges the gap: one `toFlatDeployment(d)` call maps the new shape
// back to the flat fields the UI already knows how to render.
//
// Longer-term, the UI should read directly from the envelope; this
// adapter is the minimum-churn fix to keep CI green.

import type { Deployment } from "@/lib/api/types.gen"

export type FlatStatus =
  | "deployed"
  | "deploying"
  | "failed"
  | "removing"
  | "unknown"

export interface FlatDeployment {
  // Identity. `id` is a composite of the v1alpha1 (namespace, name, version)
  // triple so existing UI that keys lists by `id` stays stable.
  id: string
  namespace: string
  name: string
  version: string
  // Target (what's being deployed) — from spec.targetRef.
  serverName: string
  resourceType: "agent" | "mcp" | string
  // Provider + lifecycle flags.
  providerId: string
  env?: Record<string, string>
  origin: "managed" | "discovered"
  status: FlatStatus
  error?: string
  deployedAt?: string
  // Original envelope kept for cases that need it directly.
  raw: Deployment
}

export function toFlatDeployment(d: Deployment): FlatDeployment {
  const ns = d.metadata.namespace ?? "default"
  const name = d.metadata.name
  const version = d.metadata.version ?? ""
  const id = `${ns}/${name}/${version}`

  const targetName = d.spec.targetRef.name
  const targetKind = d.spec.targetRef.kind
  const resourceType = kindToResourceType(targetKind)

  const ready = findCondition(d, "Ready")
  const progressing = findCondition(d, "Progressing")
  const status = deriveFlatStatus(ready, progressing)

  return {
    id,
    namespace: ns,
    name,
    version,
    serverName: targetName,
    resourceType,
    providerId: d.spec.providerRef.name,
    env: d.spec.env,
    origin: d.metadata.annotations?.["agentregistry.solo.io/origin"] === "discovered"
      ? "discovered"
      : "managed",
    status,
    error: status === "failed" ? ready?.message : undefined,
    deployedAt: ready?.lastTransitionTime ?? d.metadata.createdAt,
    raw: d,
  }
}

// kindToResourceType maps the v1alpha1 Kind string to the lowercase
// resource-type token the UI has historically used.
function kindToResourceType(kind: string): "agent" | "mcp" | string {
  switch (kind) {
    case "Agent":
      return "agent"
    case "MCPServer":
      return "mcp"
    default:
      return kind.toLowerCase()
  }
}

interface FlatCondition {
  status?: string
  reason?: string
  message?: string
  lastTransitionTime?: string
}

function findCondition(d: Deployment, type: string): FlatCondition | undefined {
  return d.status?.conditions?.find((c) => c.type === type)
}

function deriveFlatStatus(
  ready?: FlatCondition,
  progressing?: FlatCondition,
): FlatStatus {
  if (ready?.status === "True") return "deployed"
  if (ready?.status === "False" && ready.reason === "Removing") return "removing"
  if (ready?.status === "False") return "failed"
  if (progressing?.status === "True") return "deploying"
  return "unknown"
}
