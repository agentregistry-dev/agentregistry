import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, it, expect, vi } from "vitest"
import { PromptCard } from "../prompt-card"
import type { PromptResponse } from "@/lib/api/types.gen"

const mockPrompt: PromptResponse = {
  prompt: {
    name: "code-explainer",
    description: "Explains code snippets in plain language.",
    version: "1.2.0",
    content: "You are a code explainer.",
  },
  _meta: {
    "io.modelcontextprotocol.registry/official": {
      publishedAt: "2025-04-20T00:00:00Z",
      updatedAt: "2025-07-15T00:00:00Z",
      status: "active",
      isLatest: true,
    },
  },
}

describe("PromptCard", () => {
  it("renders name and description", () => {
    render(<PromptCard prompt={mockPrompt} />)
    expect(screen.getByText("code-explainer")).toBeInTheDocument()
    expect(screen.getByText("Explains code snippets in plain language.")).toBeInTheDocument()
  })

  it("renders version", () => {
    render(<PromptCard prompt={mockPrompt} />)
    expect(screen.getByText("1.2.0")).toBeInTheDocument()
  })

  it("renders published date", () => {
    render(<PromptCard prompt={mockPrompt} />)
    expect(screen.getByText(/Apr \d+, 2025/)).toBeInTheDocument()
  })

  it("calls onClick when card is clicked", async () => {
    const onClick = vi.fn()
    render(<PromptCard prompt={mockPrompt} onClick={onClick} />)
    await userEvent.click(screen.getByText("code-explainer"))
    expect(onClick).toHaveBeenCalledOnce()
  })

  it("does not render description when not provided", () => {
    const noDesc: PromptResponse = {
      prompt: {
        name: "bare-prompt",
        version: "0.1.0",
        content: "Hello.",
      },
      _meta: {},
    }
    render(<PromptCard prompt={noDesc} />)
    expect(screen.getByText("bare-prompt")).toBeInTheDocument()
    expect(screen.getByText("0.1.0")).toBeInTheDocument()
    expect(screen.queryByText("Hello.")).not.toBeInTheDocument()
  })

  it("does not render date when no meta", () => {
    const noMeta: PromptResponse = {
      prompt: {
        name: "no-meta-prompt",
        version: "1.0.0",
        content: "Test.",
      },
      _meta: {},
    }
    render(<PromptCard prompt={noMeta} />)
    expect(screen.queryByText(/\d{4}/)).toBeNull()
  })
})
