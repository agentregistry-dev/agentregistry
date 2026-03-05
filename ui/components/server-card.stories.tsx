import type { Meta, StoryObj } from "@storybook/react-vite"
import { ServerCard } from "./server-card"
import type { ServerResponse } from "@/lib/api/types.gen"

const mockServer: ServerResponse = {
  server: {
    $schema: "https://modelcontextprotocol.io/schemas/server.json",
    name: "acme/database-server",
    title: "Database Server",
    description:
      "A production-ready MCP server that provides read and write access to PostgreSQL databases with connection pooling and query optimization.",
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

const minimalServer: ServerResponse = {
  server: {
    $schema: "https://modelcontextprotocol.io/schemas/server.json",
    name: "test/minimal-server",
    description: "A bare-bones server with no extras.",
    version: "0.0.1",
  },
  _meta: {},
}

const meta: Meta<typeof ServerCard> = {
  title: "Components/ServerCard",
  component: ServerCard,
  tags: ["autodocs"],
  decorators: [
    (Story) => (
      <div style={{ maxWidth: 500 }}>
        <Story />
      </div>
    ),
  ],
}

export default meta
type Story = StoryObj<typeof ServerCard>

export const Default: Story = {
  args: {
    server: mockServer,
  },
}

export const Minimal: Story = {
  args: {
    server: minimalServer,
  },
}

export const WithDeploy: Story = {
  args: {
    server: mockServer,
    showDeploy: true,
  },
}

export const WithDelete: Story = {
  args: {
    server: mockServer,
    showDelete: true,
  },
}

export const WithVersionCount: Story = {
  args: {
    server: mockServer,
    versionCount: 5,
  },
}
