"use client"

import { useEffect, useState, useRef } from "react"
import Link from "next/link"
import { Card } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Checkbox } from "@/components/ui/checkbox"
import { Label } from "@/components/ui/label"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { ServerCard } from "@/components/server-card"
import { SkillCard } from "@/components/skill-card"
import { AgentCard } from "@/components/agent-card"
import { ServerDetail } from "@/components/server-detail"
import { SkillDetail } from "@/components/skill-detail"
import { AgentDetail } from "@/components/agent-detail"
import { ImportDialog } from "@/components/import-dialog"
import { AddServerDialog } from "@/components/add-server-dialog"
import { ImportSkillsDialog } from "@/components/import-skills-dialog"
import { AddSkillDialog } from "@/components/add-skill-dialog"
import { ImportAgentsDialog } from "@/components/import-agents-dialog"
import { AddAgentDialog } from "@/components/add-agent-dialog"
import { adminApiClient, ServerResponse, SkillResponse, AgentResponse, ServerStats } from "@/lib/admin-api"
import MCPIcon from "@/components/icons/mcp"
import { toast } from "sonner"
import {
  Search,
  Download,
  RefreshCw,
  Plus,
  Zap,
  Bot,
  Eye,
  ArrowUpDown,
  X,
  ChevronDown,
  Filter,
} from "lucide-react"

// Grouped server type
interface GroupedServer extends ServerResponse {
  versionCount: number
  allVersions: ServerResponse[]
}

export default function AdminPage() {
  const [activeTab, setActiveTab] = useState("servers")
  const [servers, setServers] = useState<ServerResponse[]>([])
  const [groupedServers, setGroupedServers] = useState<GroupedServer[]>([])
  const [skills, setSkills] = useState<SkillResponse[]>([])
  const [agents, setAgents] = useState<AgentResponse[]>([])
  const [filteredServers, setFilteredServers] = useState<GroupedServer[]>([])
  const [filteredSkills, setFilteredSkills] = useState<SkillResponse[]>([])
  const [filteredAgents, setFilteredAgents] = useState<AgentResponse[]>([])
  const [stats, setStats] = useState<ServerStats | null>(null)
  const [searchQuery, setSearchQuery] = useState("")
  const [sortBy, setSortBy] = useState<"name" | "stars" | "date">("name")
  const [filterVerifiedOrg, setFilterVerifiedOrg] = useState(false)
  const [filterVerifiedPublisher, setFilterVerifiedPublisher] = useState(false)
  const [importDialogOpen, setImportDialogOpen] = useState(false)
  const [addServerDialogOpen, setAddServerDialogOpen] = useState(false)
  const [importSkillsDialogOpen, setImportSkillsDialogOpen] = useState(false)
  const [addSkillDialogOpen, setAddSkillDialogOpen] = useState(false)
  const [importAgentsDialogOpen, setImportAgentsDialogOpen] = useState(false)
  const [addAgentDialogOpen, setAddAgentDialogOpen] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selectedServer, setSelectedServer] = useState<ServerResponse | null>(null)
  const [selectedSkill, setSelectedSkill] = useState<SkillResponse | null>(null)
  const [selectedAgent, setSelectedAgent] = useState<AgentResponse | null>(null)
  
  // Track scroll position for restoring after navigation
  const scrollPositionRef = useRef<number>(0)
  const shouldRestoreScrollRef = useRef<boolean>(false)

  // Helper function to extract GitHub stars from server metadata
  const getStars = (server: ServerResponse): number => {
    const publisherMetadata = server.server._meta?.['io.modelcontextprotocol.registry/publisher-provided']?.['agentregistry.solo.io/metadata']
    return publisherMetadata?.stars ?? 0
  }

  // Helper function to get published date
  const getPublishedDate = (server: ServerResponse): Date | null => {
    const publishedAt = server._meta?.['io.modelcontextprotocol.registry/official']?.publishedAt
    if (!publishedAt) return null
    try {
      return new Date(publishedAt)
    } catch {
      return null
    }
  }

  // Group servers by name, keeping the latest version as the representative
  const groupServersByName = (servers: ServerResponse[]): GroupedServer[] => {
    const grouped = new Map<string, ServerResponse[]>()
    
    // Group all versions by server name
    servers.forEach((server) => {
      const name = server.server.name
      if (!grouped.has(name)) {
        grouped.set(name, [])
      }
      grouped.get(name)!.push(server)
    })
    
    // Convert to GroupedServer array, using the latest version as representative
    return Array.from(grouped.entries()).map(([name, versions]) => {
      // Sort versions by date (newest first) or version string
      const sortedVersions = [...versions].sort((a, b) => {
        const dateA = getPublishedDate(a)
        const dateB = getPublishedDate(b)
        if (dateA && dateB) {
          return dateB.getTime() - dateA.getTime()
        }
        // Fallback to version string comparison
        return b.server.version.localeCompare(a.server.version)
      })
      
      const latestVersion = sortedVersions[0]
      return {
        ...latestVersion,
        versionCount: versions.length,
        allVersions: sortedVersions,
      }
    })
  }

  // Fetch data from API
  const fetchData = async () => {
    try {
      setLoading(true)
      setError(null)
      
      // Fetch all servers (with pagination if needed)
      const allServers: ServerResponse[] = []
      let serverCursor: string | undefined
      
      do {
        const response = await adminApiClient.listServers({ 
          cursor: serverCursor, 
          limit: 100,
        })
        allServers.push(...response.servers)
        serverCursor = response.metadata.nextCursor
      } while (serverCursor)
      
      setServers(allServers)

      // Fetch all skills (with pagination if needed)
      const allSkills: SkillResponse[] = []
      let skillCursor: string | undefined
      
      do {
        const response = await adminApiClient.listSkills({ 
          cursor: skillCursor, 
          limit: 100,
        })
        allSkills.push(...response.skills)
        skillCursor = response.metadata.nextCursor
      } while (skillCursor)
      
      setSkills(allSkills)

      // Fetch all agents (with pagination if needed)
      const allAgents: AgentResponse[] = []
      let agentCursor: string | undefined
      
      do {
        const response = await adminApiClient.listAgents({ 
          cursor: agentCursor, 
          limit: 100,
        })
        allAgents.push(...response.agents)
        agentCursor = response.metadata.nextCursor
      } while (agentCursor)
      
      setAgents(allAgents)
      
      // Group servers by name
      const grouped = groupServersByName(allServers)
      setGroupedServers(grouped)
      
      // Set stats
      setStats({
        total_servers: allServers.length,
        total_server_names: grouped.length,
        active_servers: allServers.length,
        deprecated_servers: 0,
        deleted_servers: 0,
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch data")
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchData()
  }, [])

  // Restore scroll position when returning from server detail
  useEffect(() => {
    if (!selectedServer && shouldRestoreScrollRef.current) {
      // Use setTimeout to ensure DOM has updated
      setTimeout(() => {
        window.scrollTo({
          top: scrollPositionRef.current,
          behavior: 'instant' as ScrollBehavior
        })
        shouldRestoreScrollRef.current = false
      }, 0)
    }
  }, [selectedServer])

  // Handle server card click - save scroll position before navigating
  const handleServerClick = (server: GroupedServer) => {
    scrollPositionRef.current = window.scrollY
    shouldRestoreScrollRef.current = true
    setSelectedServer(server)
  }

  // Handle closing server detail - flag for scroll restoration
  const handleCloseServerDetail = () => {
    setSelectedServer(null)
  }

  // Handle server publishing
  const handlePublish = async (server: ServerResponse) => {
    try {
      await adminApiClient.publishServerStatus(server.server.name, server.server.version)
      await fetchData() // Refresh data
      toast.success(`Successfully published ${server.server.name}`)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to publish server")
    }
  }

  const handlePublishSkill = async (skill: SkillResponse) => {
    try {
      await adminApiClient.publishSkillStatus(skill.skill.name, skill.skill.version)
      await fetchData() // Refresh data
      toast.success(`Successfully published ${skill.skill.name}`)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to publish skill")
    }
  }

  const handlePublishAgent = async (agentResponse: AgentResponse) => {
    const {agent } = agentResponse;

    try {
      await adminApiClient.publishAgentStatus(agent.name, agent.version)
      await fetchData() // Refresh data
      toast.success(`Successfully published ${agent.name}`)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to publish agent")
    }
  }

  // Filter and sort servers based on search query and sort option
  useEffect(() => {
    let filtered = [...groupedServers]

    // Filter by search query
    if (searchQuery) {
      const query = searchQuery.toLowerCase()
      filtered = filtered.filter(
        (s) =>
          s.server.name.toLowerCase().includes(query) ||
          s.server.title?.toLowerCase().includes(query) ||
          s.server.description.toLowerCase().includes(query)
      )
    }

    // Filter by verified organization
    if (filterVerifiedOrg) {
      filtered = filtered.filter((s) => {
        const identityData = s.server._meta?.['io.modelcontextprotocol.registry/publisher-provided']?.['agentregistry.solo.io/metadata']?.identity
        return identityData?.org_is_verified === true
      })
    }

    // Filter by verified publisher
    if (filterVerifiedPublisher) {
      filtered = filtered.filter((s) => {
        const identityData = s.server._meta?.['io.modelcontextprotocol.registry/publisher-provided']?.['agentregistry.solo.io/metadata']?.identity
        return identityData?.publisher_identity_verified_by_jwt === true
      })
    }

    // Sort servers
    filtered.sort((a, b) => {
      switch (sortBy) {
        case "stars":
          return getStars(b) - getStars(a)
        case "date": {
          const dateA = getPublishedDate(a)
          const dateB = getPublishedDate(b)
          if (!dateA && !dateB) return 0
          if (!dateA) return 1
          if (!dateB) return -1
          return dateB.getTime() - dateA.getTime()
        }
        case "name":
        default:
          return a.server.name.localeCompare(b.server.name)
      }
    })

    setFilteredServers(filtered)
  }, [searchQuery, groupedServers, sortBy, filterVerifiedOrg, filterVerifiedPublisher])

  // Filter skills and agents based on search query
  useEffect(() => {
    if (searchQuery) {
      const query = searchQuery.toLowerCase()
      
      // Filter skills
      const filteredSk = skills.filter(
        (s) =>
          s.skill.name.toLowerCase().includes(query) ||
          s.skill.title?.toLowerCase().includes(query) ||
          s.skill.description.toLowerCase().includes(query)
      )
      setFilteredSkills(filteredSk)

      // Filter agents
      const filteredA = agents.filter(
        ({agent}) =>
          agent.name?.toLowerCase().includes(query) ||
          agent.modelProvider?.toLowerCase().includes(query) ||
          agent.description.toLowerCase().includes(query)
      )
      setFilteredAgents(filteredA)
    } else {
      setFilteredSkills(skills)
      setFilteredAgents(agents)
    }
  }, [searchQuery, skills, agents])

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-center">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-primary mx-auto mb-4"></div>
          <p className="text-muted-foreground">Loading registry data...</p>
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-center">
          <div className="text-red-500 text-6xl mb-4">⚠️</div>
          <h2 className="text-xl font-bold mb-2">Error Loading Registry</h2>
          <p className="text-muted-foreground mb-4">{error}</p>
          <Button onClick={fetchData}>Retry</Button>
        </div>
      </div>
    )
  }

  // Show server detail view if a server is selected
  if (selectedServer) {
    return (
      <ServerDetail
        server={selectedServer as ServerResponse & { allVersions?: ServerResponse[] }}
        onClose={handleCloseServerDetail}
        onServerCopied={fetchData}
        onPublish={handlePublish}
      />
    )
  }

  // Show skill detail view if a skill is selected
  if (selectedSkill) {
    return (
      <SkillDetail
        skill={selectedSkill}
        onClose={() => setSelectedSkill(null)}
        onPublish={handlePublishSkill}
      />
    )
  }

  // Show agent detail view if an agent is selected
  if (selectedAgent) {
    return (
      <AgentDetail
        agent={selectedAgent}
        onClose={() => setSelectedAgent(null)}
        onPublish={handlePublishAgent}
      />
    )
  }

  return (
    <main className="min-h-screen bg-background">
      {/* Stats Section */}
      {stats && (
        <div className="bg-muted/30 border-b">
          <div className="container mx-auto px-6 py-6">
            <div className="grid gap-4 md:grid-cols-3">
              <Card className="p-4 hover:shadow-md transition-all duration-200 border hover:border-primary/20">
                <div className="flex items-center gap-3">
                  <div className="p-2 bg-primary/10 rounded-lg flex items-center justify-center">
                    <span className="h-5 w-5 text-primary flex items-center justify-center">
                      <MCPIcon />
                    </span>
                  </div>
                  <div>
                    <p className="text-2xl font-bold">{stats.total_server_names}</p>
                    <p className="text-xs text-muted-foreground">Servers</p>
                  </div>
                </div>
              </Card>

              <Card className="p-4 hover:shadow-md transition-all duration-200 border hover:border-primary/20">
                <div className="flex items-center gap-3">
                  <div className="p-2 bg-primary/20 rounded-lg flex items-center justify-center">
                    <Zap className="h-5 w-5 text-primary" />
                  </div>
                  <div>
                    <p className="text-2xl font-bold">{skills.length}</p>
                    <p className="text-xs text-muted-foreground">Skills</p>
                  </div>
                </div>
              </Card>

              <Card className="p-4 hover:shadow-md transition-all duration-200 border hover:border-primary/20">
                <div className="flex items-center gap-3">
                  <div className="p-2 bg-primary/30 rounded-lg flex items-center justify-center">
                    <Bot className="h-5 w-5 text-primary" />
                  </div>
                  <div>
                    <p className="text-2xl font-bold">{agents.length}</p>
                    <p className="text-xs text-muted-foreground">Agents</p>
                  </div>
                </div>
              </Card>
            </div>
          </div>
        </div>
      )}

      <div className="container mx-auto px-6 py-8">
        <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full">
          <div className="flex items-center gap-4 mb-8">
            <TabsList>
              <TabsTrigger value="servers" className="gap-2">
                <span className="h-4 w-4 flex items-center justify-center">
                  <MCPIcon />
                </span>
                Servers
              </TabsTrigger>
              <TabsTrigger value="skills" className="gap-2">
                <Zap className="h-4 w-4" />
                Skills
              </TabsTrigger>
              <TabsTrigger value="agents" className="gap-2">
                <Bot className="h-4 w-4" />
                Agents
              </TabsTrigger>
            </TabsList>

            {/* Search */}
            <div className="flex-1 max-w-md">
              <div className="relative">
                <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
                <Input
                  placeholder="Search..."
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  className="pl-10 h-9"
                />
              </div>
            </div>

            {/* Action Buttons */}
            <div className="flex items-center gap-3 ml-auto">
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="default" className="gap-2">
                    <Plus className="h-4 w-4" />
                    Add
                    <ChevronDown className="h-4 w-4" />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem onClick={() => setAddServerDialogOpen(true)}>
                    <span className="mr-2 h-4 w-4 flex items-center justify-center">
                      <MCPIcon />
                    </span>
                    Add Server
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => setAddSkillDialogOpen(true)}>
                    <Zap className="mr-2 h-4 w-4" />
                    Add Skill
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => setAddAgentDialogOpen(true)}>
                    <Bot className="mr-2 h-4 w-4" />
                    Add Agent
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>

              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="outline" className="gap-2">
                    <Download className="h-4 w-4" />
                    Import
                    <ChevronDown className="h-4 w-4" />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem onClick={() => setImportDialogOpen(true)}>
                    <span className="mr-2 h-4 w-4 flex items-center justify-center">
                      <MCPIcon />
                    </span>
                    Import Servers
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => setImportSkillsDialogOpen(true)}>
                    <Zap className="mr-2 h-4 w-4" />
                    Import Skills
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => setImportAgentsDialogOpen(true)}>
                    <Bot className="mr-2 h-4 w-4" />
                    Import Agents
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>

              <Button
                variant="ghost"
                size="icon"
                onClick={fetchData}
                title="Refresh"
              >
                <RefreshCw className="h-4 w-4" />
              </Button>
            </div>
          </div>

          {/* Servers Tab */}
          <TabsContent value="servers">
            {/* Sort and Filter controls */}
            <div className="flex items-center justify-between mb-6">
              <div className="flex items-center gap-2">
                <ArrowUpDown className="h-4 w-4 text-muted-foreground" />
                <Select value={sortBy} onValueChange={(value: "name" | "stars" | "date") => setSortBy(value)}>
                  <SelectTrigger className="w-[180px]">
                    <SelectValue placeholder="Sort by..." />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="name">Name (A-Z)</SelectItem>
                    <SelectItem value="stars">GitHub Stars</SelectItem>
                    <SelectItem value="date">Date Published</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <div className="flex items-center gap-4">
                <Filter className="h-4 w-4 text-muted-foreground" />
                <div className="flex items-center space-x-2">
                  <Checkbox 
                    id="filter-verified-org" 
                    checked={filterVerifiedOrg}
                    onCheckedChange={(checked: boolean) => setFilterVerifiedOrg(checked)}
                  />
                  <Label 
                    htmlFor="filter-verified-org" 
                    className="text-sm font-normal cursor-pointer"
                  >
                    Verified Organization
                  </Label>
                </div>
                <div className="flex items-center space-x-2">
                  <Checkbox 
                    id="filter-verified-publisher" 
                    checked={filterVerifiedPublisher}
                    onCheckedChange={(checked: boolean) => setFilterVerifiedPublisher(checked)}
                  />
                  <Label 
                    htmlFor="filter-verified-publisher" 
                    className="text-sm font-normal cursor-pointer"
                  >
                    Verified Publisher
                  </Label>
                </div>
              </div>
            </div>

            {/* Server List */}
            <div>
              <h2 className="text-lg font-semibold mb-4">
                Servers
                <span className="text-muted-foreground ml-2">
                  ({filteredServers.length})
                </span>
              </h2>

              {filteredServers.length === 0 ? (
                <Card className="p-12">
                  <div className="text-center text-muted-foreground">
                    <div className="w-12 h-12 mx-auto mb-4 opacity-50 flex items-center justify-center">
                      <MCPIcon />
                    </div>
                    <p className="text-lg font-medium mb-2">
                      {groupedServers.length === 0
                        ? "No servers in registry"
                        : "No servers match your filters"}
                    </p>
                    <p className="text-sm mb-4">
                      {groupedServers.length === 0
                        ? "Import servers from external registries to get started"
                        : "Try adjusting your search or filter criteria"}
                    </p>
                    {groupedServers.length === 0 && (
                      <Button
                        variant="outline"
                        className="gap-2"
                        onClick={() => setImportDialogOpen(true)}
                      >
                        <Download className="h-4 w-4" />
                        Import Servers
                      </Button>
                    )}
                  </div>
                </Card>
              ) : (
                <div className="grid gap-4">
                  {filteredServers.map((server, index) => (
                    <ServerCard
                      key={`${server.server.name}-${server.server.version}-${index}`}
                      server={server}
                      versionCount={server.versionCount}
                      onClick={() => handleServerClick(server)}
                      showPublish={true}
                      onPublish={handlePublish}
                    />
                  ))}
                </div>
              )}
            </div>
          </TabsContent>

          {/* Skills Tab */}
          <TabsContent value="skills">
            {/* Skills List */}
            <div>
              <h2 className="text-lg font-semibold mb-4">
                Skills
                <span className="text-muted-foreground ml-2">
                  ({filteredSkills.length})
                </span>
              </h2>

              {filteredSkills.length === 0 ? (
                <Card className="p-12">
                  <div className="text-center text-muted-foreground">
                    <div className="w-12 h-12 mx-auto mb-4 opacity-50 flex items-center justify-center text-primary">
                      <Zap className="w-12 h-12" />
                    </div>
                    <p className="text-lg font-medium mb-2">
                      {skills.length === 0
                        ? "No skills in registry"
                        : "No skills match your filters"}
                    </p>
                    <p className="text-sm mb-4">
                      {skills.length === 0
                        ? "Import skills from external sources to get started"
                        : "Try adjusting your search or filter criteria"}
                    </p>
                    {skills.length === 0 && (
                      <Button
                        variant="outline"
                        className="gap-2"
                        onClick={() => setImportSkillsDialogOpen(true)}
                      >
                        <Download className="h-4 w-4" />
                        Import Skills
                      </Button>
                    )}
                  </div>
                </Card>
              ) : (
                <div className="grid gap-4">
                  {filteredSkills.map((skill, index) => (
                    <SkillCard
                      key={`${skill.skill.name}-${skill.skill.version}-${index}`}
                      skill={skill}
                      onClick={() => setSelectedSkill(skill)}
                      showPublish={true}
                      onPublish={handlePublishSkill}
                    />
                  ))}
                </div>
              )}
            </div>
          </TabsContent>

          {/* Agents Tab */}
          <TabsContent value="agents">
            {/* Agents List */}
            <div>
              <h2 className="text-lg font-semibold mb-4">
                Agents
                <span className="text-muted-foreground ml-2">
                  ({filteredAgents.length})
                </span>
              </h2>

              {filteredAgents.length === 0 ? (
                <Card className="p-12">
                  <div className="text-center text-muted-foreground">
                    <div className="w-12 h-12 mx-auto mb-4 opacity-50 flex items-center justify-center text-primary">
                      <Bot className="w-12 h-12" />
                    </div>
                    <p className="text-lg font-medium mb-2">
                      {agents.length === 0
                        ? "No agents in registry"
                        : "No agents match your filters"}
                    </p>
                    <p className="text-sm mb-4">
                      {agents.length === 0
                        ? "Import agents from external sources to get started"
                        : "Try adjusting your search or filter criteria"}
                    </p>
                    {agents.length === 0 && (
                      <Button
                        variant="outline"
                        className="gap-2"
                        onClick={() => setImportAgentsDialogOpen(true)}
                      >
                        <Download className="h-4 w-4" />
                        Import Agents
                      </Button>
                    )}
                  </div>
                </Card>
              ) : (
                <div className="grid gap-4">
                  {filteredAgents.map((agent, index) => (
                    <AgentCard
                      key={`${agent.agent.name}-${agent.agent.version}-${index}`}
                      agent={agent}
                      onClick={() => setSelectedAgent(agent)}
                      showPublish={true}
                      onPublish={handlePublishAgent}
                    />
                  ))}
                </div>
              )}
            </div>
          </TabsContent>
        </Tabs>
      </div>

      {/* Server Dialogs */}
      <ImportDialog
        open={importDialogOpen}
        onOpenChange={setImportDialogOpen}
        onImportComplete={fetchData}
      />
      <AddServerDialog
        open={addServerDialogOpen}
        onOpenChange={setAddServerDialogOpen}
        onServerAdded={fetchData}
      />

      {/* Skill Dialogs */}
      <ImportSkillsDialog
        open={importSkillsDialogOpen}
        onOpenChange={setImportSkillsDialogOpen}
        onImportComplete={() => {}}
      />
      <AddSkillDialog
        open={addSkillDialogOpen}
        onOpenChange={setAddSkillDialogOpen}
        onSkillAdded={fetchData}
      />

      {/* Agent Dialogs */}
      <ImportAgentsDialog
        open={importAgentsDialogOpen}
        onOpenChange={setImportAgentsDialogOpen}
        onImportComplete={() => {}}
      />
      <AddAgentDialog
        open={addAgentDialogOpen}
        onOpenChange={setAddAgentDialogOpen}
        onAgentAdded={() => {}}
      />

    </main>
  )
}
