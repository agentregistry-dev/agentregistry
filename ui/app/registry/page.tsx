"use client"

import { useEffect, useState } from "react"
import Link from "next/link"
import { Card } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { ServerCard } from "@/components/server-card"
import { SkillCard } from "@/components/skill-card"
import { AgentCard } from "@/components/agent-card"
import { ServerDetail } from "@/components/server-detail"
import { SkillDetail } from "@/components/skill-detail"
import { AgentDetail } from "@/components/agent-detail"
import { DeleteConfirmationDialog } from "@/components/delete-confirmation-dialog"
import { adminApiClient, ServerResponse, SkillResponse, AgentResponse } from "@/lib/admin-api"
import MCPIcon from "@/components/icons/mcp"
import { Search, Zap, Bot, Settings } from "lucide-react"
import { Button } from "@/components/ui/button"

export default function RegistryPage() {
  const [activeTab, setActiveTab] = useState("servers")
  const [servers, setServers] = useState<ServerResponse[]>([])
  const [skills, setSkills] = useState<SkillResponse[]>([])
  const [agents, setAgents] = useState<AgentResponse[]>([])
  const [filteredServers, setFilteredServers] = useState<ServerResponse[]>([])
  const [filteredSkills, setFilteredSkills] = useState<SkillResponse[]>([])
  const [filteredAgents, setFilteredAgents] = useState<AgentResponse[]>([])
  const [searchQuery, setSearchQuery] = useState("")
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [serverToDelete, setServerToDelete] = useState<ServerResponse | null>(null)
  const [selectedServer, setSelectedServer] = useState<ServerResponse | null>(null)
  const [selectedSkill, setSelectedSkill] = useState<SkillResponse | null>(null)
  const [selectedAgent, setSelectedAgent] = useState<AgentResponse | null>(null)

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
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch data")
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchData()
  }, [])

  // Filter servers, skills, and agents based on search query
  useEffect(() => {
    if (searchQuery) {
      const query = searchQuery.toLowerCase()
      
      // Filter servers
      const filteredS = servers.filter(
        (s) =>
          s.server.name.toLowerCase().includes(query) ||
          s.server.title?.toLowerCase().includes(query) ||
          s.server.description.toLowerCase().includes(query)
      )
      setFilteredServers(filteredS)

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
        (a) =>
          a.agent.name.toLowerCase().includes(query) ||
          a.agent.title?.toLowerCase().includes(query) ||
          a.agent.description.toLowerCase().includes(query)
      )
      setFilteredAgents(filteredA)
    } else {
      setFilteredServers(servers)
      setFilteredSkills(skills)
      setFilteredAgents(agents)
    }
  }, [searchQuery, servers, skills, agents])

  // Handle server publishing
  const handlePublishServer = async (server: ServerResponse) => {
    try {
      await adminApiClient.createServer(server.server)
      alert(`Server "${server.server.name}" published successfully!`)
      await fetchData() // Refresh data
    } catch (err) {
      alert(err instanceof Error ? err.message : "Failed to publish server")
    }
  }

  // Handle skill publishing
  const handlePublishSkill = async (skill: SkillResponse) => {
    try {
      await adminApiClient.publishSkill(skill.skill)
      alert(`Skill "${skill.skill.name}" published successfully!`)
      await fetchData() // Refresh data
    } catch (err) {
      alert(err instanceof Error ? err.message : "Failed to publish skill")
    }
  }

  // Handle agent publishing
  const handlePublishAgent = async (agent: AgentResponse) => {
    try {
      await adminApiClient.publishAgent(agent.agent)
      alert(`Agent "${agent.agent.name}" published successfully!`)
      await fetchData() // Refresh data
    } catch (err) {
      alert(err instanceof Error ? err.message : "Failed to publish agent")
    }
  }

  // Handle server deletion - open dialog
  const handleDeleteServer = (server: ServerResponse) => {
    setServerToDelete(server)
    setDeleteDialogOpen(true)
  }

  // Confirm and execute deletion
  const confirmDeleteServer = async () => {
    if (!serverToDelete) return

    const serverKey = `${serverToDelete.server.name}@${serverToDelete.server.version}`

    try {
      setDeleting(true)
      await adminApiClient.deleteServer(serverToDelete.server.name, serverToDelete.server.version)
      
      // Remove from local state
      setServers(servers.filter(s => 
        `${s.server.name}@${s.server.version}` !== serverKey
      ))
      
      // Close dialog
      setDeleteDialogOpen(false)
      setServerToDelete(null)
    } catch (err) {
      alert(err instanceof Error ? err.message : "Failed to delete server")
    } finally {
      setDeleting(false)
    }
  }

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-center">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-primary mx-auto mb-4"></div>
          <p className="text-muted-foreground">Loading registry...</p>
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

  // Show skill detail view if a skill is selected
  if (selectedSkill) {
    return (
      <SkillDetail
        skill={selectedSkill}
        onClose={() => setSelectedSkill(null)}
      />
    )
  }

  // Show agent detail view if an agent is selected
  if (selectedAgent) {
    return (
      <AgentDetail
        agent={selectedAgent}
        onClose={() => setSelectedAgent(null)}
      />
    )
  }

  return (
    <main className="min-h-screen bg-background">
      {/* Navigation Bar */}
      <nav className="border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60 sticky top-0 z-50">
        <div className="container mx-auto px-6">
          <div className="flex items-center justify-between h-20">
            <Link href="/" className="flex items-center">
              <img 
                src="/arlogo.png" 
                alt="Agent Registry" 
                width={180} 
                height={60}
                className="h-12 w-auto"
              />
            </Link>
            
            <div className="flex items-center gap-6">
              <Link 
                href="/" 
                className="text-sm font-medium text-muted-foreground hover:text-foreground transition-colors"
              >
                Admin
              </Link>
              <Link 
                href="/registry" 
                className="text-sm font-medium text-foreground hover:text-foreground/80 transition-colors border-b-2 border-foreground pb-1"
              >
                Registry
              </Link>
              <Link 
                href="/deployed" 
                className="text-sm font-medium text-muted-foreground hover:text-foreground transition-colors"
              >
                Live View
              </Link>
            </div>
          </div>
        </div>
      </nav>

      {/* Stats Section */}
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
                  <p className="text-2xl font-bold">{servers.length}</p>
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

      <div className="container mx-auto px-6 py-8">
        {/* Global Search */}
        <div className="mb-8">
          <div className="relative max-w-2xl mx-auto">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-5 w-5 text-muted-foreground" />
            <Input
              placeholder="Search servers, skills, agents..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="pl-10 h-12 text-base"
            />
          </div>
        </div>

        {/* Content Tabs */}
        <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full">
          <TabsList className="mb-8 mx-auto flex w-fit">
            <TabsTrigger value="servers" className="gap-2">
              <span className="h-4 w-4 flex items-center justify-center">
                <MCPIcon />
              </span>
              Servers ({filteredServers.length})
            </TabsTrigger>
            <TabsTrigger value="skills" className="gap-2">
              <Zap className="h-4 w-4" />
              Skills ({filteredSkills.length})
            </TabsTrigger>
            <TabsTrigger value="agents" className="gap-2">
              <Bot className="h-4 w-4" />
              Agents ({filteredAgents.length})
            </TabsTrigger>
          </TabsList>

          {/* Servers Tab */}
          <TabsContent value="servers">
            {filteredServers.length === 0 ? (
              <Card className="p-12">
                <div className="text-center text-muted-foreground">
                  <div className="w-12 h-12 mx-auto mb-4 opacity-50 flex items-center justify-center">
                    <MCPIcon />
                  </div>
                  <p className="text-lg font-medium mb-2">
                    {servers.length === 0
                      ? "No servers in registry"
                      : "No servers match your search"}
                  </p>
                  <p className="text-sm">
                    {servers.length === 0
                      ? "Check back later for new servers"
                      : "Try adjusting your search criteria"}
                  </p>
                </div>
              </Card>
            ) : (
              <div className="grid gap-4 max-w-5xl mx-auto">
                {filteredServers.map((server, index) => (
                  <ServerCard
                    key={`${server.server.name}-${server.server.version}-${index}`}
                    server={server}
                    showPublish={true}
                    onPublish={handlePublishServer}
                    showDelete={true}
                    onDelete={handleDeleteServer}
                    showExternalLinks={false}
                    onClick={() => setSelectedServer(server)}
                  />
                ))}
              </div>
            )}
          </TabsContent>

          {/* Skills Tab */}
          <TabsContent value="skills">
            {filteredSkills.length === 0 ? (
              <Card className="p-12">
                <div className="text-center text-muted-foreground">
                  <div className="w-12 h-12 mx-auto mb-4 opacity-50 flex items-center justify-center text-primary">
                    <Zap className="w-12 h-12" />
                  </div>
                  <p className="text-lg font-medium mb-2">
                    {skills.length === 0
                      ? "No skills in registry"
                      : "No skills match your search"}
                  </p>
                  <p className="text-sm">
                    {skills.length === 0
                      ? "Check back later for new skills"
                      : "Try adjusting your search criteria"}
                  </p>
                </div>
              </Card>
            ) : (
              <div className="grid gap-4 max-w-5xl mx-auto">
                {filteredSkills.map((skill, index) => (
                  <SkillCard
                    key={`${skill.skill.name}-${skill.skill.version}-${index}`}
                    skill={skill}
                    showPublish={true}
                    onPublish={handlePublishSkill}
                    showExternalLinks={false}
                    onClick={() => setSelectedSkill(skill)}
                  />
                ))}
              </div>
            )}
          </TabsContent>

          {/* Agents Tab */}
          <TabsContent value="agents">
            {filteredAgents.length === 0 ? (
              <Card className="p-12">
                <div className="text-center text-muted-foreground">
                  <div className="w-12 h-12 mx-auto mb-4 opacity-50 flex items-center justify-center text-primary">
                    <Bot className="w-12 h-12" />
                  </div>
                  <p className="text-lg font-medium mb-2">
                    {agents.length === 0
                      ? "No agents in registry"
                      : "No agents match your search"}
                  </p>
                  <p className="text-sm">
                    {agents.length === 0
                      ? "Check back later for new agents"
                      : "Try adjusting your search criteria"}
                  </p>
                </div>
              </Card>
            ) : (
              <div className="grid gap-4 max-w-5xl mx-auto">
                {filteredAgents.map((agent, index) => (
                  <AgentCard
                    key={`${agent.agent.name}-${agent.agent.version}-${index}`}
                    agent={agent}
                    showPublish={true}
                    onPublish={handlePublishAgent}
                    showExternalLinks={false}
                    onClick={() => setSelectedAgent(agent)}
                  />
                ))}
              </div>
            )}
          </TabsContent>
        </Tabs>
      </div>

      {/* Delete Confirmation Dialog */}
      <DeleteConfirmationDialog
        open={deleteDialogOpen}
        onOpenChange={setDeleteDialogOpen}
        onConfirm={confirmDeleteServer}
        title="Remove Server from Registry"
        itemName={serverToDelete?.server.title || serverToDelete?.server.name || ""}
        isDeleting={deleting}
      />
    </main>
  )
}

