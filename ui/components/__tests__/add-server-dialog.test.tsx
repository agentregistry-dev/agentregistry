import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { AddServerDialog } from "../add-server-dialog"
import { createServerV0 } from "@/lib/admin-api"
import { toast } from "sonner"

vi.mock("@/lib/admin-api", () => ({
  createServerV0: vi.fn(),
}))

vi.mock("sonner", () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}))

describe("AddServerDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(createServerV0).mockResolvedValue({
      data: { server: { name: "io.navteca/hello-mcp" } },
    } as never)
  })

  it("defaults new packages to oci registry type", async () => {
    const user = userEvent.setup()

    render(<AddServerDialog open onOpenChange={() => {}} onServerAdded={() => {}} />)

    await user.click(screen.getByRole("button", { name: "Add Package" }))

    expect(screen.getByDisplayValue("oci")).toBeInTheDocument()
  })

  it("prevents submit when package transport is streamable-http without URL", async () => {
    const user = userEvent.setup()

    render(<AddServerDialog open onOpenChange={() => {}} onServerAdded={() => {}} />)

    await user.type(screen.getByLabelText("Server Name *"), "io.navteca/hello-mcp")
    await user.type(screen.getByLabelText("Version *"), "0.1.8")
    await user.type(screen.getByLabelText("Description *"), "MCP server built with FastMCP")

    await user.click(screen.getByRole("button", { name: "Add Package" }))
    await user.type(screen.getByPlaceholderText("Package identifier"), "docker.io/luisgleon/my-mcp-server:0.1.8")
    await user.type(screen.getByPlaceholderText("Version"), "0.1.8")
    await user.click(screen.getByRole("radio", { name: "streamable-http" }))

    expect(
      screen.getByPlaceholderText("Transport URL (required) e.g. http://localhost:8080/mcp"),
    ).toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "Create Server" }))

    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith("Package transport URL is required for streamable-http")
    })
    expect(createServerV0).not.toHaveBeenCalled()
  })

  it("sends transport URL in package payload for streamable-http", async () => {
    const user = userEvent.setup()

    render(<AddServerDialog open onOpenChange={() => {}} onServerAdded={() => {}} />)

    await user.type(screen.getByLabelText("Server Name *"), "io.navteca/hello-mcp")
    await user.type(screen.getByLabelText("Version *"), "0.1.8")
    await user.type(screen.getByLabelText("Description *"), "MCP server built with FastMCP")

    await user.click(screen.getByRole("button", { name: "Add Package" }))
    await user.type(screen.getByPlaceholderText("Package identifier"), "docker.io/luisgleon/my-mcp-server:0.1.8")
    await user.type(screen.getByPlaceholderText("Version"), "0.1.8")
    await user.click(screen.getByRole("radio", { name: "streamable-http" }))
    await user.type(
      screen.getByPlaceholderText("Transport URL (required) e.g. http://localhost:8080/mcp"),
      "http://localhost:8080/mcp",
    )

    await user.click(screen.getByRole("button", { name: "Create Server" }))

    await waitFor(() => {
      expect(createServerV0).toHaveBeenCalledTimes(1)
    })

    const callArg = vi.mocked(createServerV0).mock.calls[0]?.[0]
    expect(callArg?.throwOnError).toBe(true)
    expect(callArg?.body.$schema).toBe("https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json")
    expect(callArg?.body.packages).toEqual([
      {
        identifier: "docker.io/luisgleon/my-mcp-server:0.1.8",
        version: "0.1.8",
        registryType: "oci",
        transport: {
          type: "streamable-http",
          url: "http://localhost:8080/mcp",
        },
      },
    ])
  })

  it("prevents submit when remote transport requires URL and it is empty", async () => {
    const user = userEvent.setup()

    render(<AddServerDialog open onOpenChange={() => {}} onServerAdded={() => {}} />)

    await user.type(screen.getByLabelText("Server Name *"), "io.navteca/hello-mcp")
    await user.type(screen.getByLabelText("Version *"), "0.1.8")
    await user.type(screen.getByLabelText("Description *"), "MCP server built with FastMCP")

    await user.click(screen.getByRole("button", { name: "Add Remote" }))
    await user.click(screen.getByRole("button", { name: "Create Server" }))

    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith("Remote URL is required for sse")
    })
    expect(createServerV0).not.toHaveBeenCalled()
  })
})
