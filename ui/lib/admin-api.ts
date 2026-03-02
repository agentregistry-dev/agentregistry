// Auto-generated API client configuration.
// Types and SDK functions are generated from the OpenAPI spec.
// Regenerate with: make gen-client

import { client } from './api/client.gen'

const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || (typeof window !== 'undefined' && window.location.origin) || ''

client.setConfig({ baseUrl: API_BASE_URL })

export { client }
export * from './api/sdk.gen'
export * from './api/types.gen'
