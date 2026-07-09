// Helpers for reading controller-written Plugin status.
//
// Readiness contract (see pkg/api/v1alpha1/plugin.go): consumers MUST treat
// the absence of a Ready=True condition (or resolvedSource == nil) as "not
// yet resolved". The controller sets Ready=False/Progressing on first
// observe, Ready=True/Resolved once pinned and scanned, and Ready=False with
// a specific reason (SourceUnresolvable, SourceUnsupported, SourceInvalid)
// on failure.

import type { Condition, Plugin } from "@/lib/api/types.gen"

export type PluginResolutionState = "ready" | "progressing" | "failed" | "unknown"

export interface PluginResolution {
  state: PluginResolutionState
  /** Short human label, e.g. "Resolved", "Progressing", "SourceInvalid". */
  label: string
  /** Failure/progress detail from the condition message, if any. */
  message?: string
  condition?: Condition
}

export function getReadyCondition(plugin: Plugin): Condition | undefined {
  return plugin.status?.conditions?.find((c) => c.type === "Ready") ?? undefined
}

export function getPluginResolution(plugin: Plugin): PluginResolution {
  const condition = getReadyCondition(plugin)
  if (!condition) {
    return { state: "unknown", label: "Not resolved" }
  }
  if (condition.status === "True") {
    return { state: "ready", label: condition.reason || "Resolved", condition }
  }
  const label = condition.reason || "Not ready"
  const state: PluginResolutionState = condition.reason === "Progressing" ? "progressing" : "failed"
  return { state, label, message: condition.message, condition }
}

/** Short (7-char) form of the pinned commit SHA, if the source is resolved. */
export function getPinnedRevision(plugin: Plugin): string | undefined {
  const resolved = plugin.status?.resolvedSource
  if (!resolved) return undefined
  if (resolved.commit) return resolved.commit.slice(0, 7)
  if (resolved.digest) {
    const hex = resolved.digest.replace(/^sha256:/, "")
    return hex.slice(0, 7)
  }
  return undefined
}

export function getPluginRepoUrl(plugin: Plugin): string | undefined {
  return plugin.spec.source?.git?.repository?.url ?? undefined
}
