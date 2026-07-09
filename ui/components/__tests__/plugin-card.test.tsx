import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, it, expect, vi } from "vitest"
import { PluginCard } from "../plugin-card"
import type { Plugin } from "@/lib/api/types.gen"

const mockPlugin: Plugin = {
  apiVersion: "ar.dev/v1alpha1",
  kind: "Plugin",
  metadata: {
    name: "repo-analyst",
    namespace: "default",
    tag: "latest",
    createdAt: "2026-06-25T00:00:00Z",
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
  },
}

describe("PluginCard", () => {
  it("renders title as heading", () => {
    render(<PluginCard plugin={mockPlugin} />)
    expect(screen.getByText("Repo Analyst")).toBeInTheDocument()
  })

  it("renders description and tag", () => {
    render(<PluginCard plugin={mockPlugin} />)
    expect(screen.getByText("Release notes, repo maps, and churn hotspot analysis.")).toBeInTheDocument()
    expect(screen.getByText("latest")).toBeInTheDocument()
  })

  it("falls back to name when title is not set", () => {
    const noTitle: Plugin = {
      ...mockPlugin,
      spec: { ...mockPlugin.spec, title: undefined },
    }
    render(<PluginCard plugin={noTitle} />)
    expect(screen.getByText("repo-analyst")).toBeInTheDocument()
  })

  it("shows Ready badge and short pinned commit when resolved", () => {
    render(<PluginCard plugin={mockPlugin} />)
    expect(screen.getByText("Ready")).toBeInTheDocument()
    expect(screen.getByText("435bf0f")).toBeInTheDocument()
  })

  it("shows Resolving badge while the controller is progressing", () => {
    const progressing: Plugin = {
      ...mockPlugin,
      status: {
        conditions: [{ type: "Ready", status: "False", reason: "Progressing", message: "resolving plugin source" }],
      },
    }
    render(<PluginCard plugin={progressing} />)
    expect(screen.getByText("Resolving")).toBeInTheDocument()
    expect(screen.queryByText("Ready")).not.toBeInTheDocument()
  })

  it("shows the failure reason when resolution failed", () => {
    const failed: Plugin = {
      ...mockPlugin,
      status: {
        conditions: [{ type: "Ready", status: "False", reason: "SourceUnresolvable", message: "repository not found" }],
      },
    }
    render(<PluginCard plugin={failed} />)
    expect(screen.getByText("SourceUnresolvable")).toBeInTheDocument()
  })

  it("shows Not resolved when there is no status yet", () => {
    const noStatus: Plugin = { ...mockPlugin, status: undefined }
    render(<PluginCard plugin={noStatus} />)
    expect(screen.getByText("Not resolved")).toBeInTheDocument()
  })

  it("renders harness badges", () => {
    render(<PluginCard plugin={mockPlugin} />)
    expect(screen.getByText("claude-code")).toBeInTheDocument()
  })

  it("calls onClick when card is clicked", async () => {
    const onClick = vi.fn()
    render(<PluginCard plugin={mockPlugin} onClick={onClick} />)
    await userEvent.click(screen.getByText("Repo Analyst"))
    expect(onClick).toHaveBeenCalledOnce()
  })

  it("shows delete button when showDelete is true", () => {
    const onDelete = vi.fn()
    render(<PluginCard plugin={mockPlugin} showDelete onDelete={onDelete} />)
    const buttons = screen.getAllByRole("button")
    const deleteBtn = buttons.find(btn => btn.querySelector(".lucide-trash2"))
    expect(deleteBtn).toBeTruthy()
  })

  it("calls onDelete without triggering onClick", async () => {
    const onDelete = vi.fn()
    const onClick = vi.fn()
    render(<PluginCard plugin={mockPlugin} showDelete onDelete={onDelete} onClick={onClick} />)
    const buttons = screen.getAllByRole("button")
    const deleteBtn = buttons.find(btn => btn.querySelector(".lucide-trash2"))
    await userEvent.click(deleteBtn!)
    expect(onDelete).toHaveBeenCalledOnce()
    expect(onClick).not.toHaveBeenCalled()
  })
})
