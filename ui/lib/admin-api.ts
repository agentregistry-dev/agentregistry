// Auto-generated API client configuration.
// Types and SDK functions are generated from the OpenAPI spec.
// Regenerate with: make gen-client

import { client } from './api/client.gen'

const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || (typeof window !== 'undefined' && window.location.origin) || ''

client.setConfig({ baseUrl: API_BASE_URL })

export { client }
export * from './api/sdk.gen'
export * from './api/types.gen'

// Legacy-shape shims for components not yet migrated to the v1alpha1
// envelope. Re-exports old symbol names (listServersV0, ServerResponse,
// etc.) mapped to the new types. See ui/lib/ui-shims.ts.
export {
  listServersV0,
  listSkillsV0,
  listAgentsV0,
  listPromptsV0,
  toServerResponse,
  toSkillResponse,
  toAgentResponse,
  toPromptResponse,
  createServerV0,
  createSkillV0,
  createPromptV0,
  deployServer,
} from './ui-shims'
export type {
  ServerResponse,
  SkillResponse,
  AgentResponse,
  PromptResponse,
  ServerJson,
  SkillJson,
  PromptJson,
  AgentJson,
  DeployServerBody,
} from './ui-shims'
