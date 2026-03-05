import type { Meta, StoryObj } from "@storybook/react-vite"
import { AgentCard } from "./agent-card"
import type { AgentResponse } from "@/lib/api/types.gen"

const mockAgent: AgentResponse = {
  agent: {
    name: "code-review-agent",
    description: "An AI agent that reviews pull requests, identifies bugs, and suggests improvements based on best practices.",
    version: "2.1.0",
    framework: "langchain",
    language: "python",
    image: "registry.example.com/code-review-agent:2.1.0",
    modelProvider: "openai",
    modelName: "gpt-4o",
    repository: {
      url: "https://github.com/example/code-review-agent",
    },
  },
  _meta: {
    "io.modelcontextprotocol.registry/official": {
      publishedAt: "2025-06-15T00:00:00Z",
      updatedAt: "2025-06-15T00:00:00Z",
      status: "active",
      isLatest: true,
    },
  },
}

const minimalAgent: AgentResponse = {
  agent: {
    name: "simple-bot",
    description: "",
    version: "0.1.0",
    framework: "custom",
    language: "go",
    image: "",
    modelProvider: "",
    modelName: "",
  },
  _meta: {},
}

const meta: Meta<typeof AgentCard> = {
  title: "Components/AgentCard",
  component: AgentCard,
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
type Story = StoryObj<typeof AgentCard>

export const Default: Story = {
  args: {
    agent: mockAgent,
  },
}

export const Minimal: Story = {
  args: {
    agent: minimalAgent,
  },
}

export const WithDelete: Story = {
  args: {
    agent: mockAgent,
    showDelete: true,
  },
}
