import type { Meta, StoryObj } from "@storybook/react-vite"
import { SkillCard } from "./skill-card"
import type { SkillResponse } from "@/lib/admin-api"

const mockSkill: SkillResponse = {
  skill: {
    name: "code-review",
    title: "Code Review",
    description:
      "Analyzes pull requests for code quality, security vulnerabilities, and adherence to best practices. Provides inline suggestions and summary reports.",
    tag: "1.3.0",
    source: {
      repository: {
        url: "https://github.com/example/code-review-skill",
      },
    },
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
    tag: "0.1.0",
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
