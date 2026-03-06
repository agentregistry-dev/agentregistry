import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, it, expect, vi } from "vitest"
import { SkillCard } from "../skill-card"
import type { SkillResponse } from "@/lib/api/types.gen"

const mockSkill: SkillResponse = {
  skill: {
    name: "code-review",
    title: "Code Review",
    description: "Analyzes pull requests for quality and security.",
    version: "1.3.0",
    repository: {
      url: "https://github.com/example/code-review-skill",
      source: "github",
    },
    websiteUrl: "https://example.com/code-review",
    packages: [
      { identifier: "code-review-core", registryType: "npm", version: "1.3.0", transport: { type: "stdio" } },
      { identifier: "code-review-cli", registryType: "npm", version: "1.3.0", transport: { type: "stdio" } },
    ],
    remotes: [{ url: "https://remote.example.com/code-review" }],
  },
  _meta: {
    "io.modelcontextprotocol.registry/official": {
      publishedAt: "2025-03-10T00:00:00Z",
      updatedAt: "2025-09-01T00:00:00Z",
      status: "active",
      isLatest: true,
    },
  },
}

describe("SkillCard", () => {
  it("renders title and name", () => {
    render(<SkillCard skill={mockSkill} />)
    expect(screen.getByText("Code Review")).toBeInTheDocument()
    expect(screen.getByText("code-review")).toBeInTheDocument()
  })

  it("renders description and version", () => {
    render(<SkillCard skill={mockSkill} />)
    expect(screen.getByText("Analyzes pull requests for quality and security.")).toBeInTheDocument()
    expect(screen.getByText("1.3.0")).toBeInTheDocument()
  })

  it("renders package and remote counts", () => {
    render(<SkillCard skill={mockSkill} />)
    expect(screen.getByText("2 packages")).toBeInTheDocument()
    expect(screen.getByText("1 remote")).toBeInTheDocument()
  })

  it("renders repository source", () => {
    render(<SkillCard skill={mockSkill} />)
    expect(screen.getByText("github")).toBeInTheDocument()
  })

  it("falls back to name when title is not set", () => {
    const noTitle: SkillResponse = {
      skill: { ...mockSkill.skill, title: undefined },
      _meta: {},
    }
    render(<SkillCard skill={noTitle} />)
    // name appears as both the heading fallback and the subtitle
    const nameElements = screen.getAllByText("code-review")
    expect(nameElements.length).toBeGreaterThanOrEqual(2)
  })

  it("calls onClick when card is clicked", async () => {
    const onClick = vi.fn()
    render(<SkillCard skill={mockSkill} onClick={onClick} />)
    await userEvent.click(screen.getByText("Code Review"))
    expect(onClick).toHaveBeenCalledOnce()
  })

  it("shows delete button when showDelete is true", () => {
    const onDelete = vi.fn()
    render(<SkillCard skill={mockSkill} showDelete onDelete={onDelete} />)
    expect(screen.getByTitle("Remove from registry")).toBeInTheDocument()
  })

  it("calls onDelete without triggering onClick", async () => {
    const onDelete = vi.fn()
    const onClick = vi.fn()
    render(<SkillCard skill={mockSkill} showDelete onDelete={onDelete} onClick={onClick} />)
    await userEvent.click(screen.getByTitle("Remove from registry"))
    expect(onDelete).toHaveBeenCalledOnce()
    expect(onClick).not.toHaveBeenCalled()
  })
})
