"use client"

import { useEffect, useState } from "react"
import Link from "next/link"
import { Card } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
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
} from "lucide-react"

export default function AdminPage() {
  const [activeTab, setActiveTab] = useState("servers")
  const [servers, setServers] = useState<ServerResponse[]>([])
  const [filteredServers, setFilteredServers] = useState<ServerResponse[]>([])
  const [stats, setStats] = useState<ServerStats | null>(null)
  const [skillsCount, setSkillsCount] = useState(0)
  const [agentsCount, setAgentsCount] = useState(0)
  const [searchQuery, setSearchQuery] = useState("")
  const [importDialogOpen, setImportDialogOpen] = useState(false)
  const [addServerDialogOpen, setAddServerDialogOpen] = useState(false)
  const [importSkillsDialogOpen, setImportSkillsDialogOpen] = useState(false)
  const [addSkillDialogOpen, setAddSkillDialogOpen] = useState(false)
  const [importAgentsDialogOpen, setImportAgentsDialogOpen] = useState(false)
  const [addAgentDialogOpen, setAddAgentDialogOpen] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selectedServer, setSelectedServer] = useState<ServerResponse | null>(null)

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
      
      // Fake stats for now (until API is implemented)
      setStats({
        total_servers: allServers.length,
        total_server_names: allServers.length,
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

  // Filter servers based on search query
  useEffect(() => {
    let filtered = servers

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

    setFilteredServers(filtered)
  }, [searchQuery, servers])

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
        server={selectedServer}
        onClose={() => setSelectedServer(null)}
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
              className="pl-10"
            />
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
            <div className="flex items-center gap-4 mb-8 justify-end">
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
                      {servers.length === 0
                        ? "No servers in registry"
                        : "No servers match your filters"}
                    </p>
                    <p className="text-sm mb-4">
                      {servers.length === 0
                        ? "Import servers from external registries to get started"
                        : "Try adjusting your search or filter criteria"}
                    </p>
                    {servers.length === 0 && (
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
                      onClick={() => setSelectedServer(server)}
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
