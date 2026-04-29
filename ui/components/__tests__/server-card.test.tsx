import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, it, expect, vi } from "vitest"
import { ServerCard } from "../server-card"
import type { ServerResponse } from "@/lib/admin-api"

const mockServer: ServerResponse = {
  server: {
    $schema: "https://modelcontextprotocol.io/schemas/server.json",
    name: "acme/database-server",
    title: "Database Server",
    description: "MCP server for PostgreSQL with connection pooling.",
    version: "3.2.1",
    repository: {
      url: "https://github.com/acme/database-server",
      source: "github",
    },
    websiteUrl: "https://acme.dev/database-server",
    packages: [
      {
        registryType: "npm",
        identifier: "@acme/database-server",
        transport: { type: "stdio" },
      },
    ],
    remotes: [
      {
        type: "streamable-http",
        url: "https://mcp.acme.dev/database",
      },
    ],
  },
  _meta: {
    "io.modelcontextprotocol.registry/official": {
      publishedAt: "2024-11-01T00:00:00Z",
      updatedAt: "2025-08-20T00:00:00Z",
      status: "active",
      isLatest: true,
    },
  },
}

describe("ServerCard", () => {
  it("renders title as heading", () => {
    render(<ServerCard server={mockServer} />)
    expect(screen.getByText("Database Server")).toBeInTheDocument()
  })

  it("renders description and version", () => {
    render(<ServerCard server={mockServer} />)
    expect(screen.getByText("MCP server for PostgreSQL with connection pooling.")).toBeInTheDocument()
    expect(screen.getByText("3.2.1")).toBeInTheDocument()
  })

  it("renders package and remote counts", () => {
    render(<ServerCard server={mockServer} />)
    // counts are shown as numbers next to icons
    const ones = screen.getAllByText("1")
    expect(ones.length).toBeGreaterThanOrEqual(2)
  })

  it("renders repository source", () => {
    render(<ServerCard server={mockServer} />)
    expect(screen.getByText("github")).toBeInTheDocument()
  })

  it("falls back to name when title is not set", () => {
    const noTitle: ServerResponse = {
      server: { ...mockServer.server, title: undefined },
      _meta: {},
    }
    render(<ServerCard server={noTitle} />)
    expect(screen.getByText("acme/database-server")).toBeInTheDocument()
  })

  it("shows version count when provided", () => {
    render(<ServerCard server={mockServer} versionCount={5} />)
    expect(screen.getByText("+4")).toBeInTheDocument()
  })

  it("calls onClick when card is clicked", async () => {
    const onClick = vi.fn()
    render(<ServerCard server={mockServer} onClick={onClick} />)
    await userEvent.click(screen.getByText("Database Server"))
    expect(onClick).toHaveBeenCalledOnce()
  })

  it("shows deploy button when showDeploy is true and server has OCI package", () => {
    const onDeploy = vi.fn()
    const ociServer: ServerResponse = {
      server: { ...mockServer.server, packages: [{ registryType: "oci", identifier: "ghcr.io/acme/db", transport: { type: "stdio" } }] },
      _meta: mockServer._meta,
    }
    render(<ServerCard server={ociServer} showDeploy onDeploy={onDeploy} />)
    const btn = screen.getByText("Deploy").closest("button")!
    expect(btn).not.toBeDisabled()
  })

  it("calls onDeploy without triggering onClick", async () => {
    const onDeploy = vi.fn()
    const onClick = vi.fn()
    const ociServer: ServerResponse = {
      server: { ...mockServer.server, packages: [{ registryType: "oci", identifier: "ghcr.io/acme/db", transport: { type: "stdio" } }] },
      _meta: mockServer._meta,
    }
    render(<ServerCard server={ociServer} showDeploy onDeploy={onDeploy} onClick={onClick} />)
    await userEvent.click(screen.getByText("Deploy"))
    expect(onDeploy).toHaveBeenCalledOnce()
    expect(onClick).not.toHaveBeenCalled()
  })

  it("disables deploy button when server has no OCI package", () => {
    const onDeploy = vi.fn()
    render(<ServerCard server={mockServer} showDeploy onDeploy={onDeploy} />)
    const btn = screen.getByText("Deploy").closest("button")!
    expect(btn).toBeDisabled()
  })

  it("shows delete button when showDelete is true", () => {
    const onDelete = vi.fn()
    const { container } = render(<ServerCard server={mockServer} showDelete onDelete={onDelete} />)
    // Trash2 icon renders an SVG inside the delete button
    const trashIcon = container.querySelector("svg.lucide-trash2")
    expect(trashIcon).toBeTruthy()
  })

  it("renders without optional fields", () => {
    const minimal: ServerResponse = {
      server: {
        $schema: "https://modelcontextprotocol.io/schemas/server.json",
        name: "test/minimal",
        description: "Bare minimum.",
        version: "0.0.1",
      },
      _meta: {},
    }
    render(<ServerCard server={minimal} />)
    expect(screen.getByText("Bare minimum.")).toBeInTheDocument()
    expect(screen.getByText("0.0.1")).toBeInTheDocument()
  })
})
