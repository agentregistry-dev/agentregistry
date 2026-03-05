import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, it, expect, vi } from "vitest"
import { ServerCard } from "../server-card"
import type { ServerResponse } from "@/lib/api/types.gen"

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
  it("renders title and name", () => {
    render(<ServerCard server={mockServer} />)
    expect(screen.getByText("Database Server")).toBeInTheDocument()
    expect(screen.getByText("acme/database-server")).toBeInTheDocument()
  })

  it("renders description and version", () => {
    render(<ServerCard server={mockServer} />)
    expect(screen.getByText("MCP server for PostgreSQL with connection pooling.")).toBeInTheDocument()
    expect(screen.getByText("3.2.1")).toBeInTheDocument()
  })

  it("renders package and remote counts", () => {
    render(<ServerCard server={mockServer} />)
    expect(screen.getByText("1 package")).toBeInTheDocument()
    expect(screen.getByText("1 remote")).toBeInTheDocument()
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
    const nameElements = screen.getAllByText("acme/database-server")
    expect(nameElements.length).toBeGreaterThanOrEqual(2)
  })

  it("shows version count when provided", () => {
    render(<ServerCard server={mockServer} versionCount={5} />)
    expect(screen.getByText("(+4 more)")).toBeInTheDocument()
  })

  it("calls onClick when card is clicked", async () => {
    const onClick = vi.fn()
    render(<ServerCard server={mockServer} onClick={onClick} />)
    await userEvent.click(screen.getByText("Database Server"))
    expect(onClick).toHaveBeenCalledOnce()
  })

  it("shows deploy button when showDeploy is true", () => {
    const onDeploy = vi.fn()
    render(<ServerCard server={mockServer} showDeploy onDeploy={onDeploy} />)
    expect(screen.getByText("Deploy")).toBeInTheDocument()
  })

  it("calls onDeploy without triggering onClick", async () => {
    const onDeploy = vi.fn()
    const onClick = vi.fn()
    render(<ServerCard server={mockServer} showDeploy onDeploy={onDeploy} onClick={onClick} />)
    await userEvent.click(screen.getByText("Deploy"))
    expect(onDeploy).toHaveBeenCalledOnce()
    expect(onClick).not.toHaveBeenCalled()
  })

  it("shows delete button when showDelete is true", () => {
    const onDelete = vi.fn()
    render(<ServerCard server={mockServer} showDelete onDelete={onDelete} />)
    expect(screen.getByTitle("Remove from registry")).toBeInTheDocument()
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
