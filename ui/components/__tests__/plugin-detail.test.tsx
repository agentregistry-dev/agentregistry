import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, it, expect } from "vitest"
import { PluginDetail } from "../plugin-detail"
import type { Plugin } from "@/lib/api/types.gen"

const resolvedPlugin: Plugin = {
  apiVersion: "ar.dev/v1alpha1",
  kind: "Plugin",
  metadata: {
    name: "repo-analyst",
    namespace: "default",
    tag: "latest",
  },
  spec: {
    title: "Repo Analyst",
    description: "Release notes, repo maps, and churn hotspot analysis.",
    harnesses: ["claude-code"],
    source: {
      type: "git",
      git: {
        repository: {
          url: "https://github.com/ilackarms/repo-analyst",
          branch: "main",
        },
      },
    },
  },
  status: {
    conditions: [{ type: "Ready", status: "True", reason: "Resolved" }],
    resolvedSource: {
      type: "git",
      commit: "435bf0f7aa11223344556677889900aabbccddee",
    },
    manifest: {
      name: "repo-analyst",
      displayName: "Repo Analyst",
      version: "1.0.0",
      description: "Analyze any git repository.",
      license: "Apache-2.0",
      author: { name: "ilackarms", email: "dev@example.com" },
      keywords: ["git", "analysis"],
    },
    inventory: {
      skills: [
        { name: "release-notes", description: "Draft release notes from merged PRs" },
        { name: "repo-map", description: "Generate an architecture map" },
      ],
      commands: ["release-notes", "repo-map"],
      agents: ["analyst"],
      mcpServers: ["repo-index"],
      hooks: [{ event: "PostToolUse", type: "command" }],
      executables: ["scripts/scan.sh"],
    },
  },
}

describe("PluginDetail", () => {
  it("renders title, name, and resolution state", () => {
    render(<PluginDetail plugin={resolvedPlugin} />)
    // title/name also appear in the manifest section, so match the header heading
    expect(screen.getByRole("heading", { name: "Repo Analyst" })).toBeInTheDocument()
    expect(screen.getAllByText("repo-analyst").length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText("Ready")).toBeInTheDocument()
  })

  it("renders the source pointer and pinned commit", () => {
    render(<PluginDetail plugin={resolvedPlugin} />)
    expect(screen.getByText("https://github.com/ilackarms/repo-analyst")).toBeInTheDocument()
    expect(screen.getByText("main")).toBeInTheDocument()
    // full pinned SHA in the source section, short form in quick info
    expect(screen.getByText("435bf0f7aa11223344556677889900aabbccddee")).toBeInTheDocument()
    expect(screen.getByText("435bf0f")).toBeInTheDocument()
  })

  it("renders the controller-parsed manifest", () => {
    render(<PluginDetail plugin={resolvedPlugin} />)
    expect(screen.getByText("Display Name")).toBeInTheDocument()
    expect(screen.getByText("1.0.0")).toBeInTheDocument()
    expect(screen.getByText("Apache-2.0")).toBeInTheDocument()
    // author name also appears inside the repo URL, so anchor on the email
    expect(screen.getByText(/dev@example.com/)).toBeInTheDocument()
    // "git" appears as both the source type and a manifest keyword
    expect(screen.getAllByText("git").length).toBeGreaterThanOrEqual(2)
    expect(screen.getByText("analysis")).toBeInTheDocument()
  })

  it("renders inventory with hooks and executables called out prominently", async () => {
    render(<PluginDetail plugin={resolvedPlugin} />)
    await userEvent.click(screen.getByRole("button", { name: "Contents" }))

    // governance risk surface
    expect(screen.getByText("Runs code on install")).toBeInTheDocument()
    expect(screen.getByText("PostToolUse")).toBeInTheDocument()
    expect(screen.getByText("scripts/scan.sh")).toBeInTheDocument()

    // inventory contents
    expect(screen.getByText("release-notes")).toBeInTheDocument()
    expect(screen.getByText("Draft release notes from merged PRs")).toBeInTheDocument()
    expect(screen.getByText("/release-notes")).toBeInTheDocument()
    expect(screen.getByText("analyst")).toBeInTheDocument()
    expect(screen.getByText("repo-index")).toBeInTheDocument()
  })

  it("omits the risk callout when there are no hooks or executables", async () => {
    const safe: Plugin = {
      ...resolvedPlugin,
      status: {
        ...resolvedPlugin.status,
        inventory: {
          skills: [{ name: "release-notes" }],
        },
      },
    }
    render(<PluginDetail plugin={safe} />)
    await userEvent.click(screen.getByRole("button", { name: "Contents" }))
    expect(screen.queryByText("Runs code on install")).not.toBeInTheDocument()
    expect(screen.getByText("release-notes")).toBeInTheDocument()
  })

  it("explains pending inventory while the source is unresolved", async () => {
    const progressing: Plugin = {
      ...resolvedPlugin,
      status: {
        conditions: [{ type: "Ready", status: "False", reason: "Progressing", message: "resolving plugin source" }],
      },
    }
    render(<PluginDetail plugin={progressing} />)
    expect(screen.getByText("Resolving source…")).toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: "Contents" }))
    expect(screen.getByText(/Inventory is populated by the controller/)).toBeInTheDocument()
  })

  it("surfaces the failure message when resolution failed", () => {
    const failed: Plugin = {
      ...resolvedPlugin,
      status: {
        conditions: [{
          type: "Ready",
          status: "False",
          reason: "SourceUnresolvable",
          message: "repository not found: https://github.com/example/nope",
        }],
      },
    }
    render(<PluginDetail plugin={failed} />)
    expect(screen.getByText("SourceUnresolvable")).toBeInTheDocument()
    expect(screen.getByText(/repository not found/)).toBeInTheDocument()
  })
})
