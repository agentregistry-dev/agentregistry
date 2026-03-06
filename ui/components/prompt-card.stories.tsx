import type { Meta, StoryObj } from "@storybook/react-vite"
import { PromptCard } from "./prompt-card"
import type { PromptResponse } from "@/lib/api/types.gen"

const mockPrompt: PromptResponse = {
  prompt: {
    name: "code-explainer",
    description:
      "Explains code snippets in plain language, breaking down complex logic into understandable steps with examples.",
    version: "1.2.0",
    content: "You are a code explainer. Given a code snippet, explain what it does step by step.",
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

const minimalPrompt: PromptResponse = {
  prompt: {
    name: "hello-prompt",
    version: "0.1.0",
    content: "Say hello.",
  },
  _meta: {},
}

const meta: Meta<typeof PromptCard> = {
  title: "Components/PromptCard",
  component: PromptCard,
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
type Story = StoryObj<typeof PromptCard>

export const Default: Story = {
  args: {
    prompt: mockPrompt,
  },
}

export const Minimal: Story = {
  args: {
    prompt: minimalPrompt,
  },
}
