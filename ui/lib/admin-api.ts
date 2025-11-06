// Admin API client for the registry management UI
// This client communicates with the /v0 API endpoints

// In development mode with Next.js dev server, use relative URL to leverage proxy
// In production (static export), API_BASE_URL is set via environment variable or defaults to current origin
const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || (typeof window !== 'undefined' && window.location.origin) || ''

// MCP Server types based on the official spec
export interface ServerJSON {
  name: string
  title?: string
  description: string
  version: string
  icons?: Array<{
    src: string
    mimeType: string
    sizes?: string[]
    theme?: 'light' | 'dark'
  }>
  packages?: Array<{
    identifier: string
    version: string
    registryType: 'npm' | 'pypi' | 'docker'
  }>
  remotes?: Array<{
    type: string
    url?: string
  }>
  repository?: {
    source: 'github' | 'gitlab' | 'bitbucket'
    url: string
  }
  websiteUrl?: string
  _meta?: {
    'io.modelcontextprotocol.registry/publisher-provided'?: {
      'agentregistry.solo.io/metadata'?: {
        stars?: number
      }
    }
  }
}

export interface RegistryExtensions {
  status: 'active' | 'deprecated' | 'deleted'
  publishedAt: string
  updatedAt: string
  isLatest: boolean
}

export interface ServerResponse {
  server: ServerJSON
  _meta: {
    'io.modelcontextprotocol.registry/official'?: RegistryExtensions
  }
}

export interface ServerListResponse {
  servers: ServerResponse[]
  metadata: {
    count: number
    nextCursor?: string
  }
}

export interface ImportRequest {
  source: string
  headers?: Record<string, string>
  update?: boolean
  skip_validation?: boolean
}

export interface ImportResponse {
  success: boolean
  message: string
}

export interface ServerStats {
  total_servers: number
  total_server_names: number
  active_servers: number
  deprecated_servers: number
  deleted_servers: number
}

class AdminApiClient {
  private baseUrl: string

  constructor(baseUrl: string = API_BASE_URL) {
    this.baseUrl = baseUrl
  }

  // List servers with pagination and filtering
  async listServers(params?: {
    cursor?: string
    limit?: number
    search?: string
    version?: string
    updated_since?: string
  }): Promise<ServerListResponse> {
    const queryParams = new URLSearchParams()
    if (params?.cursor) queryParams.append('cursor', params.cursor)
    if (params?.limit) queryParams.append('limit', params.limit.toString())
    if (params?.search) queryParams.append('search', params.search)
    if (params?.version) queryParams.append('version', params.version)
    if (params?.updated_since) queryParams.append('updated_since', params.updated_since)

    const url = `${this.baseUrl}/v0/servers${queryParams.toString() ? '?' + queryParams.toString() : ''}`
    const response = await fetch(url)
    if (!response.ok) {
      throw new Error('Failed to fetch servers')
    }
    return response.json()
  }

  // Get a specific server version
  async getServer(serverName: string, version: string = 'latest'): Promise<ServerResponse> {
    const encodedName = encodeURIComponent(serverName)
    const encodedVersion = encodeURIComponent(version)
    const response = await fetch(`${this.baseUrl}/v0/servers/${encodedName}/versions/${encodedVersion}`)
    if (!response.ok) {
      throw new Error('Failed to fetch server')
    }
    return response.json()
  }

  // Get all versions of a server
  async getServerVersions(serverName: string): Promise<ServerListResponse> {
    const encodedName = encodeURIComponent(serverName)
    const response = await fetch(`${this.baseUrl}/v0/servers/${encodedName}/versions`)
    if (!response.ok) {
      throw new Error('Failed to fetch server versions')
    }
    return response.json()
  }

  // Import servers from an external registry
  async importServers(request: ImportRequest): Promise<ImportResponse> {
    const response = await fetch(`${this.baseUrl}/v0/admin/import`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(request),
    })
    if (!response.ok) {
      const error = await response.json()
      throw new Error(error.message || 'Failed to import servers')
    }
    return response.json()
  }

  // Create a new server
  async createServer(server: ServerJSON): Promise<ServerResponse> {
    console.log('Creating server:', server)
    const response = await fetch(`${this.baseUrl}/v0/admin/servers`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(server),
    })
    
    // Get response text first so we can parse it or show it as error
    const responseText = await response.text()
    console.log('Response status:', response.status)
    console.log('Response text:', responseText.substring(0, 200))
    
    if (!response.ok) {
      let errorMessage = 'Failed to create server'
      try {
        const errorData = JSON.parse(responseText)
        errorMessage = errorData.message || errorData.detail || errorData.title || errorMessage
        if (errorData.errors && Array.isArray(errorData.errors)) {
          errorMessage += ': ' + errorData.errors.map((e: unknown) => (typeof e === 'object' && e && 'message' in e ? (e as { message: string }).message : String(e))).join(', ')
        }
      } catch {
        // If JSON parsing fails, use the text directly (truncate if too long)
        errorMessage = responseText.length > 200 
          ? responseText.substring(0, 200) + '...' 
          : responseText || `Server error: ${response.status} ${response.statusText}`
      }
      throw new Error(errorMessage)
    }
    
    // Parse successful response
    try {
      return JSON.parse(responseText)
    } catch (e) {
      console.error('Failed to parse response:', e)
      throw new Error(`Invalid response from server: ${responseText.substring(0, 100)}`)
    }
  }

  // Delete a server
  async deleteServer(serverName: string, version: string): Promise<void> {
    const encodedName = encodeURIComponent(serverName)
    const encodedVersion = encodeURIComponent(version)
    const response = await fetch(`${this.baseUrl}/v0/admin/servers/${encodedName}/versions/${encodedVersion}`, {
      method: 'DELETE',
    })
    if (!response.ok) {
      const error = await response.text()
      throw new Error(error || 'Failed to delete server')
    }
  }

  // Get registry statistics
  async getStats(): Promise<ServerStats> {
    const response = await fetch(`${this.baseUrl}/v0/admin/stats`)
    if (!response.ok) {
      throw new Error('Failed to fetch statistics')
    }
    return response.json()
  }

  // Health check
  async healthCheck(): Promise<{ status: string }> {
    const response = await fetch(`${this.baseUrl}/v0/health`)
    if (!response.ok) {
      throw new Error('Health check failed')
    }
    return response.json()
  }
}

export const adminApiClient = new AdminApiClient()

