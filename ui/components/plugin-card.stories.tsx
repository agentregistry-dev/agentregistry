import type { Meta, StoryObj } from "@storybook/react-vite"
import { PluginCard } from "./plugin-card"
import type { Plugin } from "@/lib/api/types.gen"

const resolvedPlugin: Plugin = {
  apiVersion: "ar.dev/v1alpha1",
  kind: "Plugin",
  metadata: {
    name: "repo-analyst",
    namespace: "default",
    tag: "latest",
    createdAt: "2026-06-25T00:00:00Z",
  },
  spec: {
    title: "Repo Analyst",
    description:
      "Release notes, repo maps, and churn hotspot analysis for any git repository. Bundles three skills, three commands, and a sub-agent.",
    harnesses: ["claude-code"],
    source: {
      type: "git",
      git: {
        repository: {
          url: "https://github.com/ilackarms/repo-analyst",
        },
      },
    },
  },
  status: {
    conditions: [
      {
        type: "Ready",
        status: "True",
        reason: "Resolved",
      },
    ],
    resolvedSource: {
      type: "git",
      commit: "435bf0f7aa11223344556677889900aabbccddee",
    },
    manifest: {
      name: "repo-analyst",
      displayName: "Repo Analyst",
      version: "1.0.0",
    },
    inventory: {
      skills: [
        { name: "release-notes", description: "Draft release notes from merged PRs" },
        { name: "repo-map", description: "Generate an architecture map of the repo" },
        { name: "churn-hotspots", description: "Find high-churn risk areas" },
      ],
      commands: ["release-notes", "repo-map", "churn-hotspots"],
      agents: ["analyst"],
    },
  },
}

const progressingPlugin: Plugin = {
  apiVersion: "ar.dev/v1alpha1",
  kind: "Plugin",
  metadata: {
    name: "fresh-plugin",
    namespace: "default",
    tag: "0.1.0",
  },
  spec: {
    description: "Just registered — the controller has not resolved the source yet.",
    source: {
      type: "git",
      git: {
        repository: {
          url: "https://github.com/example/fresh-plugin",
          branch: "main",
        },
      },
    },
  },
  status: {
    conditions: [
      {
        type: "Ready",
        status: "False",
        reason: "Progressing",
        message: "resolving plugin source",
      },
    ],
  },
}

const failedPlugin: Plugin = {
  apiVersion: "ar.dev/v1alpha1",
  kind: "Plugin",
  metadata: {
    name: "broken-plugin",
    namespace: "default",
    tag: "latest",
  },
  spec: {
    title: "Broken Plugin",
    description: "The source pointer could not be resolved.",
    source: {
      type: "git",
      git: {
        repository: {
          url: "https://github.com/example/does-not-exist",
        },
      },
    },
  },
  status: {
    conditions: [
      {
        type: "Ready",
        status: "False",
        reason: "SourceUnresolvable",
        message: "repository not found: https://github.com/example/does-not-exist",
      },
    ],
  },
}

const meta: Meta<typeof PluginCard> = {
  title: "Components/PluginCard",
  component: PluginCard,
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
type Story = StoryObj<typeof PluginCard>

export const Resolved: Story = {
  args: {
    plugin: resolvedPlugin,
  },
}

export const Progressing: Story = {
  args: {
    plugin: progressingPlugin,
  },
}

export const Failed: Story = {
  args: {
    plugin: failedPlugin,
  },
}

export const WithDelete: Story = {
  args: {
    plugin: resolvedPlugin,
    showDelete: true,
  },
}

export const MultipleTags: Story = {
  args: {
    plugin: resolvedPlugin,
    tagCount: 3,
  },
}
