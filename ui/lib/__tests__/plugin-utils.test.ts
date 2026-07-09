import { describe, it, expect } from "vitest"
import { getPinnedRevision, getPluginRepoUrl, getPluginResolution } from "../plugin-utils"
import type { Plugin } from "@/lib/api/types.gen"

function makePlugin(status?: Plugin["status"]): Plugin {
  return {
    apiVersion: "ar.dev/v1alpha1",
    kind: "Plugin",
    metadata: { name: "p", namespace: "default", tag: "latest" },
    spec: {
      source: {
        type: "git",
        git: { repository: { url: "https://github.com/example/p" } },
      },
    },
    status,
  }
}

describe("getPluginResolution", () => {
  const cases: {
    name: string
    status: Plugin["status"]
    wantState: string
    wantLabel: string
  }[] = [
    {
      name: "no status at all",
      status: undefined,
      wantState: "unknown",
      wantLabel: "Not resolved",
    },
    {
      name: "no Ready condition",
      status: { conditions: [{ type: "Other", status: "True" }] },
      wantState: "unknown",
      wantLabel: "Not resolved",
    },
    {
      name: "Ready=True",
      status: { conditions: [{ type: "Ready", status: "True", reason: "Resolved" }] },
      wantState: "ready",
      wantLabel: "Resolved",
    },
    {
      name: "Ready=False progressing",
      status: { conditions: [{ type: "Ready", status: "False", reason: "Progressing", message: "resolving plugin source" }] },
      wantState: "progressing",
      wantLabel: "Progressing",
    },
    {
      name: "Ready=False terminal failure",
      status: { conditions: [{ type: "Ready", status: "False", reason: "SourceInvalid", message: "no plugin.json" }] },
      wantState: "failed",
      wantLabel: "SourceInvalid",
    },
  ]

  for (const tc of cases) {
    it(tc.name, () => {
      const got = getPluginResolution(makePlugin(tc.status))
      expect(got.state).toBe(tc.wantState)
      expect(got.label).toBe(tc.wantLabel)
    })
  }

  it("carries the condition message through for failures", () => {
    const got = getPluginResolution(makePlugin({
      conditions: [{ type: "Ready", status: "False", reason: "SourceUnresolvable", message: "repo not found" }],
    }))
    expect(got.message).toBe("repo not found")
  })
})

describe("getPinnedRevision", () => {
  it("returns undefined when unresolved", () => {
    expect(getPinnedRevision(makePlugin())).toBeUndefined()
  })

  it("shortens a git commit", () => {
    const plugin = makePlugin({ resolvedSource: { type: "git", commit: "435bf0f7aa11223344556677889900aabbccddee" } })
    expect(getPinnedRevision(plugin)).toBe("435bf0f")
  })

  it("shortens an OCI digest", () => {
    const plugin = makePlugin({ resolvedSource: { type: "oci", digest: "sha256:abcdef0123456789" } })
    expect(getPinnedRevision(plugin)).toBe("abcdef0")
  })
})

describe("getPluginRepoUrl", () => {
  it("reads the git repository url", () => {
    expect(getPluginRepoUrl(makePlugin())).toBe("https://github.com/example/p")
  })

  it("returns undefined for non-git sources", () => {
    const plugin = makePlugin()
    plugin.spec.source = { type: "oci", oci: { reference: "ghcr.io/x/y@sha256:abc" } }
    expect(getPluginRepoUrl(plugin)).toBeUndefined()
  })
})
