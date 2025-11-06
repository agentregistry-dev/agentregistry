"use client"

import { useEffect, useState, useRef } from "react"
import Link from "next/link"
import { Card } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { ServerCard } from "@/components/server-card"
import { ServerDetail } from "@/components/server-detail"
import { ImportDialog } from "@/components/import-dialog"
import { AddServerDialog } from "@/components/add-server-dialog"
import { ImportSkillsDialog } from "@/components/import-skills-dialog"
import { AddSkillDialog } from "@/components/add-skill-dialog"
import { ImportAgentsDialog } from "@/components/import-agents-dialog"
import { AddAgentDialog } from "@/components/add-agent-dialog"
import { adminApiClient, ServerResponse, ServerStats } from "@/lib/admin-api"
import MCPIcon from "@/components/icons/mcp"
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
  const [filteredServers, setFilteredServers] = useState<GroupedServer[]>([])
  const [stats, setStats] = useState<ServerStats | null>(null)
  const [skillsCount, setSkillsCount] = useState(0)
  const [agentsCount, setAgentsCount] = useState(0)
  const [searchQuery, setSearchQuery] = useState("")
  const [sortBy, setSortBy] = useState<"name" | "stars" | "date">("name")
  const [importDialogOpen, setImportDialogOpen] = useState(false)
  const [addServerDialogOpen, setAddServerDialogOpen] = useState(false)
  const [importSkillsDialogOpen, setImportSkillsDialogOpen] = useState(false)
  const [addSkillDialogOpen, setAddSkillDialogOpen] = useState(false)
  const [importAgentsDialogOpen, setImportAgentsDialogOpen] = useState(false)
  const [addAgentDialogOpen, setAddAgentDialogOpen] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selectedServer, setSelectedServer] = useState<ServerResponse | null>(null)
  
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
      let cursor: string | undefined
      
      do {
        const response = await adminApiClient.listServers({ 
          cursor, 
          limit: 100,
        })
        allServers.push(...response.servers)
        cursor = response.metadata.nextCursor
      } while (cursor)
      
      setServers(allServers)
      
      // Group servers by name
      const grouped = groupServersByName(allServers)
      setGroupedServers(grouped)
      
      // Fake stats for now (until API is implemented)
      setStats({
        total_servers: allServers.length,
        total_server_names: grouped.length,
        active_servers: allServers.length,
        deprecated_servers: 0,
        deleted_servers: 0,
      })
      
      // Mock stats for Skills and Agents (until API is implemented)
      setSkillsCount(Math.floor(Math.random() * 20) + 5) // Random number between 5-24
      setAgentsCount(Math.floor(Math.random() * 15) + 3) // Random number between 3-17
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
  }, [searchQuery, groupedServers, sortBy])

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
      />
    )
  }

  return (
    <main className="min-h-screen bg-background">
      <div className="border-b">
        <div className="container mx-auto px-6 py-6">
          <div className="flex items-center justify-between mb-6">
            <div className="flex items-center gap-6">
              <img 
                src="/ui/arlogo.png" 
                alt="Agent Registry" 
                width={200} 
                height={67}
              />
              <div className="flex items-center gap-4 text-sm">
                <Link href="/" className="text-foreground font-medium">
                  Admin
                </Link>
                <Link href="/registry" className="text-muted-foreground hover:text-foreground transition-colors flex items-center gap-2">
                  <Eye className="h-4 w-4" />
                  View Registry
                </Link>
              </div>
            </div>
            <Button
              variant="outline"
              size="icon"
              onClick={fetchData}
              title="Refresh data"
            >
              <RefreshCw className="h-5 w-5" />
            </Button>
          </div>

          {/* Stats */}
          {stats && (
            <div className="grid gap-4 md:grid-cols-3 mb-6">
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
                    <p className="text-2xl font-bold">{skillsCount}</p>
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
                    <p className="text-2xl font-bold">{agentsCount}</p>
                    <p className="text-xs text-muted-foreground">Agents</p>
                  </div>
                </div>
              </Card>
            </div>
          )}
        </div>
      </div>

      <div className="container mx-auto px-6 py-8">
        {/* Global Search */}
        <div className="mb-6">
          <div className="relative max-w-2xl">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
            <Input
              placeholder="Search servers, skills, agents..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="pl-10 pr-10"
            />
            {searchQuery && (
              <button
                onClick={() => setSearchQuery("")}
                className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors"
                aria-label="Clear search"
              >
                <X className="h-4 w-4" />
              </button>
            )}
          </div>
        </div>

        <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full">
          <TabsList className="mb-8">
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

          {/* Servers Tab */}
          <TabsContent value="servers">
            {/* Actions */}
            <div className="flex items-center gap-4 mb-8 justify-between">
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
                <Button
                  variant="outline"
                  className="gap-2"
                  onClick={() => setAddServerDialogOpen(true)}
                >
                  <Plus className="h-4 w-4" />
                  Add Server
                </Button>
                <Button
                  variant="default"
                  className="gap-2"
                  onClick={() => setImportDialogOpen(true)}
                >
                  <Download className="h-4 w-4" />
                  Import Servers
                </Button>
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
                    />
                  ))}
                </div>
              )}
            </div>
          </TabsContent>

          {/* Skills Tab */}
          <TabsContent value="skills">
            {/* Actions */}
            <div className="flex items-center gap-4 mb-8 justify-end">
              <Button
                variant="outline"
                className="gap-2"
                onClick={() => setAddSkillDialogOpen(true)}
              >
                <Plus className="h-4 w-4" />
                Add Skill
              </Button>
              <Button
                variant="default"
                className="gap-2"
                onClick={() => setImportSkillsDialogOpen(true)}
              >
                <Download className="h-4 w-4" />
                Import Skills
              </Button>
            </div>

            {/* Skills List */}
            <div>
              <h2 className="text-lg font-semibold mb-4">
                Skills
                <span className="text-muted-foreground ml-2">({skillsCount})</span>
              </h2>

              <Card className="p-12">
                <div className="text-center text-muted-foreground">
                  <div className="w-12 h-12 mx-auto mb-4 opacity-50 flex items-center justify-center text-primary">
                    <Zap className="w-12 h-12" />
                  </div>
                  <p className="text-lg font-medium mb-2">Skills view coming soon</p>
                  <p className="text-sm mb-4">
                    {skillsCount} skill{skillsCount !== 1 ? 's' : ''} available in registry
                  </p>
                  <Button
                    variant="outline"
                    className="gap-2"
                    onClick={() => setImportSkillsDialogOpen(true)}
                  >
                    <Download className="h-4 w-4" />
                    Import Skills
                  </Button>
                </div>
              </Card>
            </div>
          </TabsContent>

          {/* Agents Tab */}
          <TabsContent value="agents">
            {/* Actions */}
            <div className="flex items-center gap-4 mb-8 justify-end">
              <Button
                variant="outline"
                className="gap-2"
                onClick={() => setAddAgentDialogOpen(true)}
              >
                <Plus className="h-4 w-4" />
                Add Agent
              </Button>
              <Button
                variant="default"
                className="gap-2"
                onClick={() => setImportAgentsDialogOpen(true)}
              >
                <Download className="h-4 w-4" />
                Import Agents
              </Button>
            </div>

            {/* Agents List */}
            <div>
              <h2 className="text-lg font-semibold mb-4">
                Agents
                <span className="text-muted-foreground ml-2">({agentsCount})</span>
              </h2>

              <Card className="p-12">
                <div className="text-center text-muted-foreground">
                  <div className="w-12 h-12 mx-auto mb-4 opacity-50 flex items-center justify-center text-primary">
                    <Bot className="w-12 h-12" />
                  </div>
                  <p className="text-lg font-medium mb-2">Agents view coming soon</p>
                  <p className="text-sm mb-4">
                    {agentsCount} agent{agentsCount !== 1 ? 's' : ''} available in registry
                  </p>
                  <Button
                    variant="outline"
                    className="gap-2"
                    onClick={() => setImportAgentsDialogOpen(true)}
                  >
                    <Download className="h-4 w-4" />
                    Import Agents
                  </Button>
                </div>
              </Card>
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
        onSkillAdded={() => {}}
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
