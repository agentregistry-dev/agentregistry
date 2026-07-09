import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, it, expect, vi, beforeEach } from "vitest"
import { AddPluginDialog } from "../add-plugin-dialog"
import { createPluginV0 } from "@/lib/admin-api"

vi.mock("@/lib/admin-api", () => ({
  createPluginV0: vi.fn().mockResolvedValue({ data: {} }),
}))

const mockedCreate = vi.mocked(createPluginV0)

function renderDialog(overrides?: Partial<Parameters<typeof AddPluginDialog>[0]>) {
  const props = {
    open: true,
    onOpenChange: vi.fn(),
    onPluginAdded: vi.fn(),
    ...overrides,
  }
  render(<AddPluginDialog {...props} />)
  return props
}

describe("AddPluginDialog", () => {
  beforeEach(() => {
    mockedCreate.mockClear()
  })

  it("renders the source-resolution expectation", () => {
    renderDialog()
    expect(screen.getByText("Add Plugin", { selector: "h2" })).toBeInTheDocument()
    expect(screen.getByText(/controller resolves the source to a pinned commit/)).toBeInTheDocument()
  })

  it("submits a git-source plugin through the client", async () => {
    const props = renderDialog()

    await userEvent.type(screen.getByLabelText(/Plugin Name/), "repo-analyst")
    await userEvent.type(screen.getByLabelText("Title"), "Repo Analyst")
    await userEvent.type(screen.getByLabelText("Description"), "Analyze repos")
    await userEvent.type(screen.getByLabelText("Harnesses"), "claude-code, codex")
    await userEvent.type(screen.getByLabelText(/Repository URL/), "https://github.com/ilackarms/repo-analyst")
    await userEvent.type(screen.getByLabelText("Branch"), "main")
    await userEvent.type(screen.getByLabelText("Subfolder"), "plugins/repo-analyst")

    await userEvent.click(screen.getByRole("button", { name: "Add Plugin" }))

    expect(mockedCreate).toHaveBeenCalledOnce()
    expect(mockedCreate).toHaveBeenCalledWith({
      throwOnError: true,
      body: {
        name: "repo-analyst",
        tag: "latest",
        title: "Repo Analyst",
        description: "Analyze repos",
        harnesses: ["claude-code", "codex"],
        source: {
          type: "git",
          git: {
            repository: {
              url: "https://github.com/ilackarms/repo-analyst",
              branch: "main",
              subfolder: "plugins/repo-analyst",
            },
          },
        },
      },
    })
    expect(props.onPluginAdded).toHaveBeenCalledOnce()
    expect(props.onOpenChange).toHaveBeenCalledWith(false)
  })

  it("omits optional fields that are left empty", async () => {
    renderDialog()

    await userEvent.type(screen.getByLabelText(/Plugin Name/), "minimal-plugin")
    await userEvent.type(screen.getByLabelText(/Repository URL/), "https://github.com/example/minimal")
    await userEvent.click(screen.getByRole("button", { name: "Add Plugin" }))

    expect(mockedCreate).toHaveBeenCalledWith({
      throwOnError: true,
      body: {
        name: "minimal-plugin",
        tag: "latest",
        source: {
          type: "git",
          git: {
            repository: {
              url: "https://github.com/example/minimal",
            },
          },
        },
      },
    })
  })

  it("rejects a name that is not a DNS-1123 subdomain", async () => {
    const props = renderDialog()

    await userEvent.type(screen.getByLabelText(/Plugin Name/), "Bad_Name!")
    await userEvent.type(screen.getByLabelText(/Repository URL/), "https://github.com/example/repo")
    await userEvent.click(screen.getByRole("button", { name: "Add Plugin" }))

    expect(await screen.findByText(/DNS-1123 subdomain/)).toBeInTheDocument()
    expect(mockedCreate).not.toHaveBeenCalled()
    expect(props.onPluginAdded).not.toHaveBeenCalled()
  })

  it("shows the API error when the create fails", async () => {
    mockedCreate.mockRejectedValueOnce(new Error("apply Plugin repo-analyst failed: boom"))
    const props = renderDialog()

    await userEvent.type(screen.getByLabelText(/Plugin Name/), "repo-analyst")
    await userEvent.type(screen.getByLabelText(/Repository URL/), "https://github.com/example/repo")
    await userEvent.click(screen.getByRole("button", { name: "Add Plugin" }))

    expect(await screen.findByText(/failed: boom/)).toBeInTheDocument()
    expect(props.onOpenChange).not.toHaveBeenCalledWith(false)
  })
})
