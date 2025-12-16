// Admin API client for the registry management UI
// This client communicates with the /admin/v0 API endpoints

// In development mode with Next.js dev server, use relative URL to leverage proxy
// In production (static export), API_BASE_URL is set via environment variable or defaults to current origin
const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || (typeof window !== 'undefined' && window.location.origin) || ''

// MCP Server types based on the official spec
export interface ServerJSON {
  $schema?: string
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
    transport?: {
      type: string
      url?: string
    }
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
        score?: number
        scorecard?: {
          openssf?: number
        }
        repo?: {
          forks_count?: number
          watchers_count?: number
          primary_language?: string
          tags?: string[]
          topics?: string[]
        }
        endpoint_health?: {
          last_checked_at?: string
          reachable?: boolean
          response_ms?: number
        }
        scans?: {
          container_images?: unknown[]
          dependency_health?: {
            copyleft_licenses?: number
            ecosystems?: Record<string, number>
            packages_total?: number
            unknown_licenses?: number
          }
          details?: string[]
          summary?: string
        }
        activity?: {
          created_at?: string
          pushed_at?: string
          updated_at?: string
        }
        identity?: {
          org_is_verified?: boolean
          publisher_identity_verified_by_jwt?: boolean
        }
        semver?: {
          uses_semver?: boolean
        }
        security_scanning?: {
          code_scanning_alerts?: number | null
          codeql_enabled?: boolean
          dependabot_alerts?: number | null
          dependabot_enabled?: boolean
        }
        downloads?: {
          total?: number
        }
        releases?: {
          latest_published_at?: string | null
        }
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

// Skill types
export interface SkillRepository {
  url: string
  source: string
}

export interface SkillPackageInfo {
  registryType: string
  identifier: string
  version: string
  transport: {
    type: string
  }
}

export interface SkillRemoteInfo {
  url: string
}

export interface SkillJSON {
  name: string
  title?: string
  description: string
  version: string
  status?: string
  websiteUrl?: string
  repository?: SkillRepository
  packages?: SkillPackageInfo[]
  remotes?: SkillRemoteInfo[]
}

export interface SkillRegistryExtensions {
  status: string
  publishedAt: string
  updatedAt: string
  isLatest: boolean
}

export interface SkillResponse {
  skill: SkillJSON
  _meta: {
    'io.modelcontextprotocol.registry/official'?: SkillRegistryExtensions
  }
}

export interface SkillListResponse {
  skills: SkillResponse[]
  metadata: {
    count: number
    nextCursor?: string
  }
}

// Agent types
export interface AgentJSON {
  name: string
  image: string
  language: string
  framework: string
  modelProvider: string
  modelName: string
  description: string
  updatedAt: string
  version: string
  status: string
  repository?: {
    url: string
    source: string
  }
}

export interface AgentRegistryExtensions {
  status: string
  publishedAt: string
  updatedAt: string
  isLatest: boolean
}

export interface AgentResponse {
  agent: AgentJSON
  _meta: {
    'io.modelcontextprotocol.registry/official'?: AgentRegistryExtensions
  }
}

export interface AgentListResponse {
  agents: AgentResponse[]
  metadata: {
    count: number
    nextCursor?: string
  }
}

class AdminApiClient {
  private baseUrl: string

  constructor(baseUrl: string = API_BASE_URL) {
    this.baseUrl = baseUrl
  }

  // List servers with pagination and filtering (ADMIN - shows all servers)
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

    const url = `${this.baseUrl}/admin/v0/servers${queryParams.toString() ? '?' + queryParams.toString() : ''}`
    const response = await fetch(url)
    if (!response.ok) {
      throw new Error('Failed to fetch servers')
    }
    return response.json()
  }

  // List PUBLISHED servers only (PUBLIC endpoint)
  async listPublishedServers(params?: {
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
      throw new Error('Failed to fetch published servers')
    }
    return response.json()
  }

  // Get a specific server version
  async getServer(serverName: string, version: string = 'latest'): Promise<ServerResponse> {
    const encodedName = encodeURIComponent(serverName)
    const encodedVersion = encodeURIComponent(version)
    const response = await fetch(`${this.baseUrl}/admin/v0/servers/${encodedName}/versions/${encodedVersion}`)
    if (!response.ok) {
      throw new Error('Failed to fetch server')
    }
    return response.json()
  }

  // Get all versions of a server
  async getServerVersions(serverName: string): Promise<ServerListResponse> {
    const encodedName = encodeURIComponent(serverName)
    const response = await fetch(`${this.baseUrl}/admin/v0/servers/${encodedName}/versions`)
    if (!response.ok) {
      throw new Error('Failed to fetch server versions')
    }
    return response.json()
  }

  // Import servers from an external registry
  async importServers(request: ImportRequest): Promise<ImportResponse> {
    const response = await fetch(`${this.baseUrl}/admin/v0/import`, {
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
    const response = await fetch(`${this.baseUrl}/admin/v0/servers`, {
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
    const response = await fetch(`${this.baseUrl}/admin/v0/servers/${encodedName}/versions/${encodedVersion}`, {
      method: 'DELETE',
    })
    if (!response.ok) {
      const error = await response.text()
      throw new Error(error || 'Failed to delete server')
    }
  }

  // Get registry statistics
  async getStats(): Promise<ServerStats> {
    const response = await fetch(`${this.baseUrl}/admin/v0/stats`)
    if (!response.ok) {
      throw new Error('Failed to fetch statistics')
    }
    return response.json()
  }

  // Health check
  async healthCheck(): Promise<{ status: string }> {
    const response = await fetch(`${this.baseUrl}/admin/v0/health`)
    if (!response.ok) {
      throw new Error('Health check failed')
    }
    return response.json()
  }

  // ===== Skills API =====

  // List skills with pagination and filtering (ADMIN - shows all skills)
  async listSkills(params?: {
    cursor?: string
    limit?: number
    search?: string
    version?: string
    updated_since?: string
  }): Promise<SkillListResponse> {
    const queryParams = new URLSearchParams()
    if (params?.cursor) queryParams.append('cursor', params.cursor)
    if (params?.limit) queryParams.append('limit', params.limit.toString())
    if (params?.search) queryParams.append('search', params.search)
    if (params?.version) queryParams.append('version', params.version)
    if (params?.updated_since) queryParams.append('updated_since', params.updated_since)

    const url = `${this.baseUrl}/admin/v0/skills${queryParams.toString() ? '?' + queryParams.toString() : ''}`
    const response = await fetch(url)
    if (!response.ok) {
      throw new Error('Failed to fetch skills')
    }
    return response.json()
  }

  // List PUBLISHED skills only (PUBLIC endpoint)
  async listPublishedSkills(params?: {
    cursor?: string
    limit?: number
    search?: string
    version?: string
    updated_since?: string
  }): Promise<SkillListResponse> {
    const queryParams = new URLSearchParams()
    if (params?.cursor) queryParams.append('cursor', params.cursor)
    if (params?.limit) queryParams.append('limit', params.limit.toString())
    if (params?.search) queryParams.append('search', params.search)
    if (params?.version) queryParams.append('version', params.version)
    if (params?.updated_since) queryParams.append('updated_since', params.updated_since)

    const url = `${this.baseUrl}/v0/skills${queryParams.toString() ? '?' + queryParams.toString() : ''}`
    const response = await fetch(url)
    if (!response.ok) {
      throw new Error('Failed to fetch published skills')
    }
    return response.json()
  }

  // Get a specific skill version
  async getSkill(skillName: string, version: string = 'latest'): Promise<SkillResponse> {
    const encodedName = encodeURIComponent(skillName)
    const encodedVersion = encodeURIComponent(version)
    const response = await fetch(`${this.baseUrl}/admin/v0/skills/${encodedName}/versions/${encodedVersion}`)
    if (!response.ok) {
      throw new Error('Failed to fetch skill')
    }
    return response.json()
  }

  // Get all versions of a skill
  async getSkillVersions(skillName: string): Promise<SkillListResponse> {
    const encodedName = encodeURIComponent(skillName)
    const response = await fetch(`${this.baseUrl}/admin/v0/skills/${encodedName}/versions`)
    if (!response.ok) {
      throw new Error('Failed to fetch skill versions')
    }
    return response.json()
  }

  // Create a new skill
  async createSkill(skill: SkillJSON): Promise<SkillResponse> {
    const response = await fetch(`${this.baseUrl}/admin/v0/skills`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(skill),
    })
    if (!response.ok) {
      const errorData = await response.json().catch(() => ({}))
      throw new Error(errorData.detail || 'Failed to create skill')
    }
    return response.json()
  }

  // ===== Agents API =====

  // List agents with pagination and filtering (ADMIN - shows all agents)
  async listAgents(params?: {
    cursor?: string
    limit?: number
    search?: string
    version?: string
    updated_since?: string
  }): Promise<AgentListResponse> {
    const queryParams = new URLSearchParams()
    if (params?.cursor) queryParams.append('cursor', params.cursor)
    if (params?.limit) queryParams.append('limit', params.limit.toString())
    if (params?.search) queryParams.append('search', params.search)
    if (params?.version) queryParams.append('version', params.version)
    if (params?.updated_since) queryParams.append('updated_since', params.updated_since)

    const url = `${this.baseUrl}/admin/v0/agents${queryParams.toString() ? '?' + queryParams.toString() : ''}`
    const response = await fetch(url)
    if (!response.ok) {
      throw new Error('Failed to fetch agents')
    }
    return response.json()
  }

  // List PUBLISHED agents only (PUBLIC endpoint)
  async listPublishedAgents(params?: {
    cursor?: string
    limit?: number
    search?: string
    version?: string
    updated_since?: string
  }): Promise<AgentListResponse> {
    const queryParams = new URLSearchParams()
    if (params?.cursor) queryParams.append('cursor', params.cursor)
    if (params?.limit) queryParams.append('limit', params.limit.toString())
    if (params?.search) queryParams.append('search', params.search)
    if (params?.version) queryParams.append('version', params.version)
    if (params?.updated_since) queryParams.append('updated_since', params.updated_since)

    const url = `${this.baseUrl}/v0/agents${queryParams.toString() ? '?' + queryParams.toString() : ''}`
    const response = await fetch(url)
    if (!response.ok) {
      throw new Error('Failed to fetch published agents')
    }
    return response.json()
  }

  // Get a specific agent version
  async getAgent(agentName: string, version: string = 'latest'): Promise<AgentResponse> {
    const encodedName = encodeURIComponent(agentName)
    const encodedVersion = encodeURIComponent(version)
    const response = await fetch(`${this.baseUrl}/admin/v0/agents/${encodedName}/versions/${encodedVersion}`)
    if (!response.ok) {
      throw new Error('Failed to fetch agent')
    }
    return response.json()
  }

  // Get all versions of an agent
  async getAgentVersions(agentName: string): Promise<AgentListResponse> {
    const encodedName = encodeURIComponent(agentName)
    const response = await fetch(`${this.baseUrl}/admin/v0/agents/${encodedName}/versions`)
    if (!response.ok) {
      throw new Error('Failed to fetch agent versions')
    }
    return response.json()
  }

  // Create an agent in the registry
  async createAgent(agent: AgentJSON): Promise<AgentResponse> {
    const response = await fetch(`${this.baseUrl}/admin/v0/agents`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(agent),
    })
    if (!response.ok) {
      const errorData = await response.json().catch(() => ({}))
      throw new Error(errorData.detail || 'Failed to create agent')
    }
    return response.json()
  }

  // ===== Publish Status API =====

  // Publish a server (change status to published)
  async publishServerStatus(serverName: string, version: string): Promise<void> {
    const encodedName = encodeURIComponent(serverName)
    const encodedVersion = encodeURIComponent(version)
    const response = await fetch(`${this.baseUrl}/admin/v0/servers/${encodedName}/versions/${encodedVersion}/publish`, {
      method: 'POST',
    })
    if (!response.ok) {
      const error = await response.text()
      throw new Error(error || 'Failed to publish server')
    }
  }

  // Unpublish a server (change status to unpublished)
  async unpublishServerStatus(serverName: string, version: string): Promise<void> {
    const encodedName = encodeURIComponent(serverName)
    const encodedVersion = encodeURIComponent(version)
    const response = await fetch(`${this.baseUrl}/admin/v0/servers/${encodedName}/versions/${encodedVersion}/unpublish`, {
      method: 'POST',
    })
    if (!response.ok) {
      const error = await response.text()
      throw new Error(error || 'Failed to unpublish server')
    }
  }

  // Publish a skill (change status to published)
  async publishSkillStatus(skillName: string, version: string): Promise<void> {
    const encodedName = encodeURIComponent(skillName)
    const encodedVersion = encodeURIComponent(version)
    const response = await fetch(`${this.baseUrl}/admin/v0/skills/${encodedName}/versions/${encodedVersion}/publish`, {
      method: 'POST',
    })
    if (!response.ok) {
      const error = await response.text()
      throw new Error(error || 'Failed to publish skill')
    }
  }

  // Unpublish a skill (change status to unpublished)
  async unpublishSkillStatus(skillName: string, version: string): Promise<void> {
    const encodedName = encodeURIComponent(skillName)
    const encodedVersion = encodeURIComponent(version)
    const response = await fetch(`${this.baseUrl}/admin/v0/skills/${encodedName}/versions/${encodedVersion}/unpublish`, {
      method: 'POST',
    })
    if (!response.ok) {
      const error = await response.text()
      throw new Error(error || 'Failed to unpublish skill')
    }
  }

  // Publish an agent (change status to published)
  async publishAgentStatus(agentName: string, version: string): Promise<void> {
    const encodedName = encodeURIComponent(agentName)
    const encodedVersion = encodeURIComponent(version)
    const response = await fetch(`${this.baseUrl}/admin/v0/agents/${encodedName}/versions/${encodedVersion}/publish`, {
      method: 'POST',
    })
    if (!response.ok) {
      const error = await response.text()
      throw new Error(error || 'Failed to publish agent')
    }
  }

  // Unpublish an agent (change status to unpublished)
  async unpublishAgentStatus(agentName: string, version: string): Promise<void> {
    const encodedName = encodeURIComponent(agentName)
    const encodedVersion = encodeURIComponent(version)
    const response = await fetch(`${this.baseUrl}/admin/v0/agents/${encodedName}/versions/${encodedVersion}/unpublish`, {
      method: 'POST',
    })
    if (!response.ok) {
      const error = await response.text()
      throw new Error(error || 'Failed to unpublish agent')
    }
  }

  // ===== Deployments API =====

  // Deploy a server
  async deployServer(params: {
    serverName: string
    version?: string
    config?: Record<string, string>
    preferRemote?: boolean
    resourceType?: 'mcp' | 'agent'
  }): Promise<void> {
    const response = await fetch(`${this.baseUrl}/admin/v0/deployments`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        serverName: params.serverName,
        version: params.version || 'latest',
        config: params.config || {},
        preferRemote: params.preferRemote || false,
        resourceType: params.resourceType || 'mcp',
      }),
    })
    if (!response.ok) {
      const errorData = await response.json().catch(() => ({}))
      throw new Error(errorData.message || errorData.detail || 'Failed to deploy server')
    }
  }

  // Get all deployments
  async listDeployments(): Promise<Array<{
    serverName: string
    version: string
    deployedAt: string
    updatedAt: string
    status: string
    config: Record<string, string>
    preferRemote: boolean
    resourceType: string
  }>> {
    const response = await fetch(`${this.baseUrl}/admin/v0/deployments`)
    if (!response.ok) {
      throw new Error('Failed to fetch deployments')
    }
    const data = await response.json()
    return data.deployments || []
  }

  // Remove a deployment
  async removeDeployment(serverName: string, version: string): Promise<void> {
    const encodedName = encodeURIComponent(serverName)
    const encodedVersion = encodeURIComponent(version)
    const response = await fetch(`${this.baseUrl}/admin/v0/deployments/${encodedName}/versions/${encodedVersion}`, {
      method: 'DELETE',
    })
    if (!response.ok) {
      const errorData = await response.json().catch(() => ({}))
      throw new Error(errorData.message || errorData.detail || 'Failed to remove deployment')
    }
  }
}

export const adminApiClient = new AdminApiClient()

