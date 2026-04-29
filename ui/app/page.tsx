"use client"

import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Checkbox } from "@/components/ui/checkbox"
import { Label } from "@/components/ui/label"
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
import { PromptCard } from "@/components/prompt-card"
import { ServerDetail } from "@/components/server-detail"
import { SkillDetail } from "@/components/skill-detail"
import { AgentDetail } from "@/components/agent-detail"
import { PromptDetail } from "@/components/prompt-detail"
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet"
import { ImportDialog } from "@/components/import-dialog"
import { AddServerDialog } from "@/components/add-server-dialog"
import { AddSkillDialog } from "@/components/add-skill-dialog"
import { AddAgentDialog } from "@/components/add-agent-dialog"
import { AddPromptDialog } from "@/components/add-prompt-dialog"
import { DeployDialog } from "@/components/deploy-dialog"
import { listServersV0, listSkillsV0, listAgentsV0, listPromptsV0, ServerResponse, SkillResponse, AgentResponse, PromptResponse } from "@/lib/admin-api"
import MCPIcon from "@/components/icons/mcp"
import {
  Search,
  Download,
  RefreshCw,
  Plus,
  Zap,
  Bot,
  ArrowUpDown,
  ChevronDown,
  FileText,
} from "lucide-react"

// Grouped server type
interface GroupedServer extends ServerResponse {
  versionCount: number
  allVersions: ServerResponse[]
}

// Grouped prompt type
interface GroupedPrompt extends PromptResponse {
  versionCount: number
  allVersions: PromptResponse[]
}

// Grouped skill type
interface GroupedSkill extends SkillResponse {
  versionCount: number
  allVersions: SkillResponse[]
}

// Grouped agent type
interface GroupedAgent extends AgentResponse {
  versionCount: number
  allVersions: AgentResponse[]
}

type TabKey = "servers" | "skills" | "agents" | "prompts"

const TAB_CONFIG: { key: TabKey; label: string; icon: React.ReactNode }[] = [
  { key: "servers", label: "Servers", icon: <MCPIcon /> },
  { key: "skills", label: "Skills", icon: <Zap className="h-3.5 w-3.5" /> },
  { key: "agents", label: "Agents", icon: <Bot className="h-3.5 w-3.5" /> },
  { key: "prompts", label: "Prompts", icon: <FileText className="h-3.5 w-3.5" /> },
]

export default function AdminPage() {
  const [activeTab, setActiveTab] = useState<TabKey>("servers")
  const [servers, setServers] = useState<ServerResponse[]>([])
  const [groupedServers, setGroupedServers] = useState<GroupedServer[]>([])
  const [groupedSkills, setGroupedSkills] = useState<GroupedSkill[]>([])
  const [groupedAgents, setGroupedAgents] = useState<GroupedAgent[]>([])
  const [groupedPrompts, setGroupedPrompts] = useState<GroupedPrompt[]>([])
  const [filteredServers, setFilteredServers] = useState<GroupedServer[]>([])
  const [filteredSkills, setFilteredSkills] = useState<GroupedSkill[]>([])
  const [filteredAgents, setFilteredAgents] = useState<GroupedAgent[]>([])
  const [filteredPrompts, setFilteredPrompts] = useState<GroupedPrompt[]>([])
  const [searchQuery, setSearchQuery] = useState("")
  const [sortBy, setSortBy] = useState<"name" | "stars" | "date">("name")
  const [filterVerifiedOrg, setFilterVerifiedOrg] = useState(false)
  const [filterVerifiedPublisher, setFilterVerifiedPublisher] = useState(false)
  const [importDialogOpen, setImportDialogOpen] = useState(false)
  const [addServerDialogOpen, setAddServerDialogOpen] = useState(false)
  const [addSkillDialogOpen, setAddSkillDialogOpen] = useState(false)
  const [addAgentDialogOpen, setAddAgentDialogOpen] = useState(false)
  const [addPromptDialogOpen, setAddPromptDialogOpen] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selectedServer, setSelectedServer] = useState<ServerResponse | null>(null)
  const [selectedSkill, setSelectedSkill] = useState<GroupedSkill | null>(null)
  const [selectedAgent, setSelectedAgent] = useState<GroupedAgent | null>(null)
  const [selectedPrompt, setSelectedPrompt] = useState<GroupedPrompt | null>(null)
  const [deployServerTarget, setDeployServerTarget] = useState<ServerResponse | null>(null)
  const [deployAgentTarget, setDeployAgentTarget] = useState<AgentResponse | null>(null)

  const getStars = (server: ServerResponse): number => {
    const publisherProvided = server.server._meta?.['io.modelcontextprotocol.registry/publisher-provided'] as Record<string, unknown> | undefined
    const metadata = publisherProvided?.['aregistry.ai/metadata'] as Record<string, unknown> | undefined
    return (metadata?.stars as number) ?? 0
  }

  const getPublishedDate = (server: ServerResponse): Date | null => {
    const publishedAt = server._meta?.['io.modelcontextprotocol.registry/official']?.publishedAt
    if (!publishedAt) return null
    try {
      return new Date(publishedAt)
    } catch {
      return null
    }
  }

  const groupServersByName = (servers: ServerResponse[]): GroupedServer[] => {
    const grouped = new Map<string, ServerResponse[]>()

    servers.forEach((server) => {
      const name = server.server.name
      if (!grouped.has(name)) {
        grouped.set(name, [])
      }
      grouped.get(name)!.push(server)
    })

    return Array.from(grouped.entries()).map(([, versions]) => {
      const sortedVersions = [...versions].sort((a, b) => {
        const dateA = getPublishedDate(a)
        const dateB = getPublishedDate(b)
        if (dateA && dateB) {
          return dateB.getTime() - dateA.getTime()
        }
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

  const groupSkillsByName = (skills: SkillResponse[]): GroupedSkill[] => {
    const grouped = new Map<string, SkillResponse[]>()

    skills.forEach((skill) => {
      const name = skill.skill.name
      if (!grouped.has(name)) {
        grouped.set(name, [])
      }
      grouped.get(name)!.push(skill)
    })

    return Array.from(grouped.entries()).map(([, versions]) => {
      const sortedVersions = [...versions].sort((a, b) => {
        const dateA = a._meta?.['io.modelcontextprotocol.registry/official']?.publishedAt
        const dateB = b._meta?.['io.modelcontextprotocol.registry/official']?.publishedAt
        if (dateA && dateB) {
          return new Date(dateB).getTime() - new Date(dateA).getTime()
        }
        return (b.skill.version || '').localeCompare(a.skill.version || '')
      })

      const latestVersion = sortedVersions[0]
      return {
        ...latestVersion,
        versionCount: versions.length,
        allVersions: sortedVersions,
      }
    })
  }

  const groupAgentsByName = (agents: AgentResponse[]): GroupedAgent[] => {
    const grouped = new Map<string, AgentResponse[]>()

    agents.forEach((agent) => {
      const name = agent.agent.name
      if (!grouped.has(name)) {
        grouped.set(name, [])
      }
      grouped.get(name)!.push(agent)
    })

    return Array.from(grouped.entries()).map(([, versions]) => {
      const sortedVersions = [...versions].sort((a, b) => {
        const dateA = a._meta?.['io.modelcontextprotocol.registry/official']?.publishedAt
        const dateB = b._meta?.['io.modelcontextprotocol.registry/official']?.publishedAt
        if (dateA && dateB) {
          return new Date(dateB).getTime() - new Date(dateA).getTime()
        }
        return (b.agent.version || '').localeCompare(a.agent.version || '')
      })

      const latestVersion = sortedVersions[0]
      return {
        ...latestVersion,
        versionCount: versions.length,
        allVersions: sortedVersions,
      }
    })
  }

  const groupPromptsByName = (prompts: PromptResponse[]): GroupedPrompt[] => {
    const grouped = new Map<string, PromptResponse[]>()

    prompts.forEach((prompt) => {
      const name = prompt.prompt.name
      if (!grouped.has(name)) {
        grouped.set(name, [])
      }
      grouped.get(name)!.push(prompt)
    })

    return Array.from(grouped.entries()).map(([, versions]) => {
      const sortedVersions = [...versions].sort((a, b) => {
        const dateA = a._meta?.['io.modelcontextprotocol.registry/official']?.publishedAt
        const dateB = b._meta?.['io.modelcontextprotocol.registry/official']?.publishedAt
        if (dateA && dateB) {
          return new Date(dateB).getTime() - new Date(dateA).getTime()
        }
        return (b.prompt.version || '').localeCompare(a.prompt.version || '')
      })

      const latestVersion = sortedVersions[0]
      return {
        ...latestVersion,
        versionCount: versions.length,
        allVersions: sortedVersions,
      }
    })
  }

  const fetchData = async () => {
    try {
      setLoading(true)
      setError(null)

      const allServers: ServerResponse[] = []
      let serverCursor: string | undefined
      do {
        const { data: serverData } = await listServersV0({
          query: { cursor: serverCursor, limit: 100 },
          throwOnError: true,
        })
        allServers.push(...serverData.servers)
        serverCursor = serverData.metadata.nextCursor
      } while (serverCursor)
      setServers(allServers)

      const allSkills: SkillResponse[] = []
      let skillCursor: string | undefined
      do {
        const { data: skillData } = await listSkillsV0({
          query: { cursor: skillCursor, limit: 100 },
          throwOnError: true,
        })
        allSkills.push(...skillData.skills)
        skillCursor = skillData.metadata.nextCursor
      } while (skillCursor)
      const groupedS = groupSkillsByName(allSkills)
      setGroupedSkills(groupedS)

      const allAgents: AgentResponse[] = []
      let agentCursor: string | undefined
      do {
        const { data: agentData } = await listAgentsV0({
          query: { cursor: agentCursor, limit: 100 },
          throwOnError: true,
        })
        allAgents.push(...agentData.agents)
        agentCursor = agentData.metadata.nextCursor
      } while (agentCursor)
      const groupedA = groupAgentsByName(allAgents)
      setGroupedAgents(groupedA)

      const allPrompts: PromptResponse[] = []
      let promptCursor: string | undefined
      do {
        const { data: promptData } = await listPromptsV0({
          query: { cursor: promptCursor, limit: 100 },
          throwOnError: true,
        })
        allPrompts.push(...promptData.prompts)
        promptCursor = promptData.metadata.nextCursor
      } while (promptCursor)
      const groupedP = groupPromptsByName(allPrompts)
      setGroupedPrompts(groupedP)

      const grouped = groupServersByName(allServers)
      setGroupedServers(grouped)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch data")
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { fetchData() }, [])

  const isSheetOpen = !!(selectedServer || selectedSkill || selectedAgent || selectedPrompt)
  const closeSheet = () => {
    setSelectedServer(null)
    setSelectedSkill(null)
    setSelectedAgent(null)
    setSelectedPrompt(null)
  }

  // Filter and sort servers
  useEffect(() => {
    let filtered = [...groupedServers]

    if (searchQuery) {
      const query = searchQuery.toLowerCase()
      filtered = filtered.filter(
        (s) =>
          s.server.name.toLowerCase().includes(query) ||
          s.server.title?.toLowerCase().includes(query) ||
          s.server.description?.toLowerCase().includes(query)
      )
    }

    if (filterVerifiedOrg) {
      filtered = filtered.filter((s) => {
        const publisherProvided = s.server._meta?.['io.modelcontextprotocol.registry/publisher-provided'] as Record<string, unknown> | undefined
        const metadata = publisherProvided?.['aregistry.ai/metadata'] as Record<string, unknown> | undefined
        const identityData = metadata?.identity as Record<string, unknown> | undefined
        return identityData?.org_is_verified === true
      })
    }

    if (filterVerifiedPublisher) {
      filtered = filtered.filter((s) => {
        const publisherProvided = s.server._meta?.['io.modelcontextprotocol.registry/publisher-provided'] as Record<string, unknown> | undefined
        const metadata = publisherProvided?.['aregistry.ai/metadata'] as Record<string, unknown> | undefined
        const identityData = metadata?.identity as Record<string, unknown> | undefined
        return identityData?.publisher_identity_verified_by_jwt === true
      })
    }

    filtered.sort((a, b) => {
      switch (sortBy) {
        case "stars": return getStars(b) - getStars(a)
        case "date": {
          const dateA = getPublishedDate(a)
          const dateB = getPublishedDate(b)
          if (!dateA && !dateB) return 0
          if (!dateA) return 1
          if (!dateB) return -1
          return dateB.getTime() - dateA.getTime()
        }
        default: return a.server.name.localeCompare(b.server.name)
      }
    })

    setFilteredServers(filtered)
  }, [searchQuery, groupedServers, sortBy, filterVerifiedOrg, filterVerifiedPublisher])

  // Filter skills, agents, prompts
  useEffect(() => {
    if (searchQuery) {
      const query = searchQuery.toLowerCase()
      setFilteredSkills(groupedSkills.filter((s) =>
        s.skill.name.toLowerCase().includes(query) ||
        s.skill.title?.toLowerCase().includes(query) ||
        s.skill.description?.toLowerCase().includes(query)
      ))
      setFilteredAgents(groupedAgents.filter(({agent}) =>
        agent.name?.toLowerCase().includes(query) ||
        agent.modelProvider?.toLowerCase().includes(query) ||
        agent.description?.toLowerCase().includes(query)
      ))
      setFilteredPrompts(groupedPrompts.filter(({prompt}) =>
        prompt.name?.toLowerCase().includes(query) ||
        prompt.description?.toLowerCase().includes(query) ||
        prompt.content?.toLowerCase().includes(query)
      ))
    } else {
      setFilteredSkills(groupedSkills)
      setFilteredAgents(groupedAgents)
      setFilteredPrompts(groupedPrompts)
    }
  }, [searchQuery, groupedSkills, groupedAgents, groupedPrompts])

  const getCount = (tab: TabKey) => {
    switch (tab) {
      case "servers": return groupedServers.length
      case "skills": return groupedSkills.length
      case "agents": return groupedAgents.length
      case "prompts": return groupedPrompts.length
    }
  }

  if (loading) {
    return (
      <div className="min-h-[60vh] flex items-center justify-center">
        <div className="text-center space-y-3">
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-primary border-t-transparent mx-auto" />
          <p className="text-sm text-muted-foreground">Loading registry...</p>
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="min-h-[60vh] flex items-center justify-center">
        <div className="text-center space-y-3">
          <p className="text-lg font-semibold">Failed to load registry</p>
          <p className="text-sm text-muted-foreground max-w-md">{error}</p>
          <Button onClick={fetchData} size="sm">Retry</Button>
        </div>
      </div>
    )
  }

  return (
    <main className="bg-background">
      <div className="container mx-auto px-6">
        {/* Tab bar with counts */}
        <div className="flex items-center justify-between border-b pt-4">
          <div className="flex items-center gap-1">
            {TAB_CONFIG.map(({ key, label, icon }) => (
              <button
                key={key}
                onClick={() => setActiveTab(key)}
                className={`group relative flex items-center gap-2 px-4 py-3 text-[15px] font-medium transition-colors ${
                  activeTab === key
                    ? "text-foreground"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                <span className={`h-4 w-4 flex items-center justify-center transition-colors ${
                  activeTab === key ? "text-primary" : "text-muted-foreground group-hover:text-foreground"
                }`}>
                  {icon}
                </span>
                {label}
                <span className={`text-[13px] tabular-nums px-1.5 py-0.5 rounded-full transition-colors ${
                  activeTab === key
                    ? "bg-primary/10 text-primary"
                    : "bg-muted text-muted-foreground"
                }`}>
                  {getCount(key)}
                </span>
                {activeTab === key && (
                  <span className="absolute bottom-0 left-2 right-2 h-0.5 bg-primary rounded-full" />
                )}
              </button>
            ))}
          </div>

          <div className="flex items-center gap-2 pb-2">
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button size="sm" className="gap-1.5 h-8">
                  <Plus className="h-3.5 w-3.5" />
                  Add
                  <ChevronDown className="h-3 w-3 opacity-60" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onClick={() => setAddServerDialogOpen(true)}>
                  <span className="mr-2 h-4 w-4 flex items-center justify-center"><MCPIcon /></span>
                  Server
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => setAddSkillDialogOpen(true)}>
                  <Zap className="mr-2 h-4 w-4" />
                  Skill
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => setAddAgentDialogOpen(true)}>
                  <Bot className="mr-2 h-4 w-4" />
                  Agent
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => setAddPromptDialogOpen(true)}>
                  <FileText className="mr-2 h-4 w-4" />
                  Prompt
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>

            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm" className="gap-1.5 h-8">
                  <Download className="h-3.5 w-3.5" />
                  Import
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onClick={() => setImportDialogOpen(true)}>
                  <span className="mr-2 h-4 w-4 flex items-center justify-center"><MCPIcon /></span>
                  Import Servers
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>

            <Button variant="ghost" size="icon" className="h-8 w-8" onClick={fetchData} title="Refresh">
              <RefreshCw className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>

        {/* Search and filters */}
        <div className="flex items-center gap-3 py-4">
          <div className="relative flex-1 max-w-sm">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
            <Input
              placeholder={`Search ${activeTab}...`}
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="pl-9 h-10 text-[15px]"
            />
          </div>

          {activeTab === "servers" && (
            <>
              <div className="flex items-center gap-1.5 ml-auto">
                <ArrowUpDown className="h-3.5 w-3.5 text-muted-foreground" />
                <Select value={sortBy} onValueChange={(value: "name" | "stars" | "date") => setSortBy(value)}>
                  <SelectTrigger className="w-[140px] h-8 text-sm">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="name">Name</SelectItem>
                    <SelectItem value="stars">Stars</SelectItem>
                    <SelectItem value="date">Published</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <div className="flex items-center gap-4 pl-3 border-l">
                <div className="flex items-center gap-1.5">
                  <Checkbox
                    id="filter-verified-org"
                    checked={filterVerifiedOrg}
                    onCheckedChange={(checked: boolean) => setFilterVerifiedOrg(checked)}
                    className="h-3.5 w-3.5"
                  />
                  <Label htmlFor="filter-verified-org" className="text-xs cursor-pointer text-muted-foreground">
                    Verified Org
                  </Label>
                </div>
                <div className="flex items-center gap-1.5">
                  <Checkbox
                    id="filter-verified-publisher"
                    checked={filterVerifiedPublisher}
                    onCheckedChange={(checked: boolean) => setFilterVerifiedPublisher(checked)}
                    className="h-3.5 w-3.5"
                  />
                  <Label htmlFor="filter-verified-publisher" className="text-xs cursor-pointer text-muted-foreground">
                    Verified Publisher
                  </Label>
                </div>
              </div>
            </>
          )}
        </div>

        {/* Results */}
        <div className="pb-12">
          {activeTab === "servers" && (
            filteredServers.length === 0 ? (
              <EmptyState
                icon={<span className="h-8 w-8 flex items-center justify-center text-muted-foreground"><MCPIcon /></span>}
                title={groupedServers.length === 0 ? "No servers in registry" : "No servers match your filters"}
                description={groupedServers.length === 0 ? "Import servers from external registries to get started" : "Try adjusting your search or filter criteria"}
                action={groupedServers.length === 0 ? (
                  <Button variant="outline" size="sm" className="gap-1.5" onClick={() => setImportDialogOpen(true)}>
                    <Download className="h-3.5 w-3.5" /> Import Servers
                  </Button>
                ) : undefined}
              />
            ) : (
              <div className="divide-y">
                {filteredServers.map((server, index) => (
                  <ServerCard
                    key={`${server.server.name}-${server.server.version}-${index}`}
                    server={server}
                    versionCount={server.versionCount}
                    onClick={() => setSelectedServer(server)}
                    showDeploy
                    onDeploy={(s) => setDeployServerTarget(s)}
                  />
                ))}
              </div>
            )
          )}

          {activeTab === "skills" && (
            filteredSkills.length === 0 ? (
              <EmptyState
                icon={<Zap className="h-8 w-8 text-muted-foreground" />}
                title={groupedSkills.length === 0 ? "No skills in registry" : "No skills match your filters"}
                description={groupedSkills.length === 0 ? "Publish skills to get started" : "Try adjusting your search"}
                action={groupedSkills.length === 0 ? (
                  <Button variant="outline" size="sm" className="gap-1.5" onClick={() => setAddSkillDialogOpen(true)}>
                    <Plus className="h-3.5 w-3.5" /> Add Skill
                  </Button>
                ) : undefined}
              />
            ) : (
              <div className="divide-y">
                {filteredSkills.map((skill, index) => (
                  <SkillCard
                    key={`${skill.skill.name}-${skill.skill.version}-${index}`}
                    skill={skill}
                    versionCount={skill.versionCount}
                    onClick={() => setSelectedSkill(skill)}
                  />
                ))}
              </div>
            )
          )}

          {activeTab === "agents" && (
            filteredAgents.length === 0 ? (
              <EmptyState
                icon={<Bot className="h-8 w-8 text-muted-foreground" />}
                title={groupedAgents.length === 0 ? "No agents in registry" : "No agents match your filters"}
                description={groupedAgents.length === 0 ? "Create agents to get started" : "Try adjusting your search"}
                action={groupedAgents.length === 0 ? (
                  <Button variant="outline" size="sm" className="gap-1.5" onClick={() => setAddAgentDialogOpen(true)}>
                    <Plus className="h-3.5 w-3.5" /> Add Agent
                  </Button>
                ) : undefined}
              />
            ) : (
              <div className="divide-y">
                {filteredAgents.map((agent, index) => (
                  <AgentCard
                    key={`${agent.agent.name}-${agent.agent.version}-${index}`}
                    agent={agent}
                    versionCount={agent.versionCount}
                    onClick={() => setSelectedAgent(agent)}
                    showDeploy
                    onDeploy={(a) => setDeployAgentTarget(a)}
                  />
                ))}
              </div>
            )
          )}

          {activeTab === "prompts" && (
            filteredPrompts.length === 0 ? (
              <EmptyState
                icon={<FileText className="h-8 w-8 text-muted-foreground" />}
                title={groupedPrompts.length === 0 ? "No prompts in registry" : "No prompts match your filters"}
                description={groupedPrompts.length === 0 ? "Add prompts to get started" : "Try adjusting your search"}
                action={groupedPrompts.length === 0 ? (
                  <Button variant="outline" size="sm" className="gap-1.5" onClick={() => setAddPromptDialogOpen(true)}>
                    <Plus className="h-3.5 w-3.5" /> Add Prompt
                  </Button>
                ) : undefined}
              />
            ) : (
              <div className="divide-y">
                {filteredPrompts.map((prompt, index) => (
                  <PromptCard
                    key={`${prompt.prompt.name}-${prompt.prompt.version}-${index}`}
                    prompt={prompt}
                    versionCount={prompt.versionCount}
                    onClick={() => setSelectedPrompt(prompt)}
                  />
                ))}
              </div>
            )
          )}
        </div>
      </div>

      <ImportDialog open={importDialogOpen} onOpenChange={setImportDialogOpen} onImportComplete={fetchData} />
      <AddServerDialog open={addServerDialogOpen} onOpenChange={setAddServerDialogOpen} onServerAdded={fetchData} />
      <AddSkillDialog open={addSkillDialogOpen} onOpenChange={setAddSkillDialogOpen} onSkillAdded={fetchData} />
      <AddAgentDialog open={addAgentDialogOpen} onOpenChange={setAddAgentDialogOpen} onAgentAdded={() => {}} />
      <AddPromptDialog open={addPromptDialogOpen} onOpenChange={setAddPromptDialogOpen} onPromptAdded={fetchData} />

      <DeployDialog
        open={!!deployServerTarget}
        onOpenChange={(open) => { if (!open) setDeployServerTarget(null) }}
        resourceType="mcp"
        server={deployServerTarget}
        onDeploySuccess={fetchData}
      />
      <DeployDialog
        open={!!deployAgentTarget}
        onOpenChange={(open) => { if (!open) setDeployAgentTarget(null) }}
        resourceType="agent"
        agent={deployAgentTarget}
        onDeploySuccess={fetchData}
      />

      <Sheet open={isSheetOpen} onOpenChange={(open) => !open && closeSheet()}>
        <SheetContent side="right" className="w-full sm:max-w-2xl overflow-y-auto">
          <SheetTitle className="sr-only">
            {selectedServer ? (selectedServer.server.title || selectedServer.server.name) :
             selectedAgent ? selectedAgent.agent.name :
             selectedSkill ? (selectedSkill.skill.title || selectedSkill.skill.name) :
             selectedPrompt ? selectedPrompt.prompt.name : 'Details'}
          </SheetTitle>
          {selectedServer && (
            <ServerDetail
              server={selectedServer as ServerResponse & { allVersions?: ServerResponse[] }}
              onServerCopied={fetchData}
            />
          )}
          {selectedSkill && <SkillDetail skill={selectedSkill} allVersions={selectedSkill.allVersions} />}
          {selectedAgent && <AgentDetail agent={selectedAgent} allVersions={selectedAgent.allVersions} />}
          {selectedPrompt && <PromptDetail prompt={selectedPrompt} allVersions={selectedPrompt.allVersions} />}
        </SheetContent>
      </Sheet>
    </main>
  )
}

function EmptyState({ icon, title, description, action }: {
  icon: React.ReactNode
  title: string
  description: string
  action?: React.ReactNode
}) {
  return (
    <div className="flex flex-col items-center justify-center py-20 text-center">
      <div className="mb-4 opacity-40">{icon}</div>
      <p className="text-base font-medium mb-1">{title}</p>
      <p className="text-sm text-muted-foreground mb-4 max-w-xs">{description}</p>
      {action}
    </div>
  )
}
