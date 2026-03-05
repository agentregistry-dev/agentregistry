import type { Meta, StoryObj } from "@storybook/react-vite"
import { SkillCard } from "./skill-card"
import type { SkillResponse } from "@/lib/api/types.gen"

const mockSkill: SkillResponse = {
  skill: {
    name: "code-review",
    title: "Code Review",
    description:
      "Analyzes pull requests for code quality, security vulnerabilities, and adherence to best practices. Provides inline suggestions and summary reports.",
    version: "1.3.0",
    category: "development",
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

const minimalSkill: SkillResponse = {
  skill: {
    name: "hello-world",
    description: "A simple starter skill.",
    version: "0.1.0",
  },
  _meta: {},
}

const meta: Meta<typeof SkillCard> = {
  title: "Components/SkillCard",
  component: SkillCard,
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
type Story = StoryObj<typeof SkillCard>

export const Default: Story = {
  args: {
    skill: mockSkill,
  },
}

export const Minimal: Story = {
  args: {
    skill: minimalSkill,
  },
}

export const WithDelete: Story = {
  args: {
    skill: mockSkill,
    showDelete: true,
  },
}

export const WithoutExternalLinks: Story = {
  args: {
    skill: mockSkill,
    showExternalLinks: false,
  },
}
