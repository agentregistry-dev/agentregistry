"use client"

import { useState } from "react"
import { AgentResponse } from "@/lib/admin-api"
import { Badge } from "@/components/ui/badge"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import {
  Calendar,
  Bot,
  Code,
  Cpu,
  Brain,
  Languages,
  Box,
  Clock,
  Github,
  ExternalLink,
  History,
} from "lucide-react"

interface AgentDetailProps {
  agent: AgentResponse
  allVersions?: AgentResponse[]
}

export function AgentDetail({ agent, allVersions: allVersionsProp }: AgentDetailProps) {
  const [activeTab, setActiveTab] = useState("overview")
  const [selectedVersion, setSelectedVersion] = useState<AgentResponse>(agent)

  const allVersions = allVersionsProp || [agent]

  const { agent: agentData, _meta } = selectedVersion
  const official = _meta?.['io.modelcontextprotocol.registry/official']

  const handleVersionChange = (version: string) => {
    const newVersion = allVersions.find(v => v.agent.version === version)
    if (newVersion) setSelectedVersion(newVersion)
  }

  const formatDate = (dateString: string) => {
    try {
      return new Date(dateString).toLocaleString('en-US', {
        year: 'numeric',
        month: 'long',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
      })
    } catch {
      return dateString
    }
  }

  return (
    <div className="space-y-6">
        {/* Header */}
        <div className="flex items-start gap-4">
          <div className="w-12 h-12 rounded bg-primary/8 flex items-center justify-center flex-shrink-0">
            <Bot className="h-6 w-6 text-primary" />
          </div>
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 mb-1">
              <h1 className="text-2xl font-bold truncate">{agentData.name}</h1>
              <Badge variant="outline" className="text-sm">{agentData.framework}</Badge>
              <Badge variant="secondary" className="text-sm">{agentData.language}</Badge>
            </div>
            {agentData.description && (
              <p className="text-[15px] text-muted-foreground">{agentData.description}</p>
            )}
          </div>
        </div>

        {/* Version selector */}
        {allVersions.length > 1 && (
          <div className="flex items-center gap-3 px-3 py-2 bg-accent/50 border border-primary/10 rounded-md">
            <History className="h-4 w-4 text-muted-foreground" />
            <span className="text-sm">{allVersions.length} versions</span>
            <Select value={selectedVersion.agent.version} onValueChange={handleVersionChange}>
              <SelectTrigger className="w-[160px] h-7 text-sm">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {allVersions.map((version) => (
                  <SelectItem key={version.agent.version} value={version.agent.version}>
                    {version.agent.version}
                    {version.agent.version === agent.agent.version && " (latest)"}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}

        {/* Quick info */}
        <div className="flex flex-wrap gap-2">
          <span className="flex items-center gap-1.5 px-2.5 py-1 bg-muted rounded text-sm">
            <span className="font-mono">{agentData.version}</span>
            {allVersions.length > 1 && (
              <Badge variant="secondary" className="text-[10px] px-1 py-0 h-3.5">{allVersions.length} total</Badge>
            )}
          </span>
          <span className="flex items-center gap-1.5 px-2.5 py-1 bg-muted rounded text-sm">
            {agentData.status}
          </span>
          {official?.publishedAt && (
            <span className="flex items-center gap-1.5 px-2.5 py-1 bg-muted rounded text-sm">
              <Calendar className="h-3 w-3 text-muted-foreground" />
              {formatDate(official.publishedAt)}
            </span>
          )}
          {official?.updatedAt && (
            <span className="flex items-center gap-1.5 px-2.5 py-1 bg-muted rounded text-sm">
              <Clock className="h-3 w-3 text-muted-foreground" />
              {formatDate(official.updatedAt)}
            </span>
          )}
        </div>

        <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full">
          <TabsList className="mb-4">
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="technical">Technical</TabsTrigger>
            <TabsTrigger value="raw">Raw</TabsTrigger>
          </TabsList>

          <TabsContent value="overview" className="space-y-6">
            {agentData.description && (
              <section>
                <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-2">Description</h3>
                <p className="text-[15px] leading-relaxed">{agentData.description}</p>
              </section>
            )}

            <section>
              <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-3">Details</h3>
              <div className="grid grid-cols-2 gap-4">
                <div className="flex items-center gap-2.5">
                  <Languages className="h-4 w-4 text-muted-foreground" />
                  <div>
                    <p className="text-xs text-muted-foreground">Language</p>
                    <p className="text-[15px] font-medium">{agentData.language}</p>
                  </div>
                </div>
                <div className="flex items-center gap-2.5">
                  <Box className="h-4 w-4 text-muted-foreground" />
                  <div>
                    <p className="text-xs text-muted-foreground">Framework</p>
                    <p className="text-[15px] font-medium">{agentData.framework}</p>
                  </div>
                </div>
                <div className="flex items-center gap-2.5">
                  <Brain className="h-4 w-4 text-muted-foreground" />
                  <div>
                    <p className="text-xs text-muted-foreground">Model Provider</p>
                    <p className="text-[15px] font-medium">{agentData.modelProvider}</p>
                  </div>
                </div>
                <div className="flex items-center gap-2.5">
                  <Cpu className="h-4 w-4 text-muted-foreground" />
                  <div>
                    <p className="text-xs text-muted-foreground">Model</p>
                    <p className="text-[15px] font-medium font-mono">{agentData.modelName}</p>
                  </div>
                </div>
              </div>
            </section>

            {agentData.repository?.url && (
              <section>
                <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-2">Repository</h3>
                <a
                  href={agentData.repository.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex items-center gap-1.5 text-sm text-primary hover:underline"
                >
                  <Github className="h-3.5 w-3.5" />
                  {agentData.repository.url}
                  <ExternalLink className="h-3 w-3" />
                </a>
              </section>
            )}
          </TabsContent>

          <TabsContent value="technical" className="space-y-6">
            {agentData.repository?.url && (
              <section>
                <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-2">Source Repository</h3>
                <a
                  href={agentData.repository.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex items-center gap-1.5 text-sm text-primary hover:underline font-mono"
                >
                  {agentData.repository.url}
                  <ExternalLink className="h-3 w-3" />
                </a>
                {agentData.repository.source && (
                  <p className="text-xs text-muted-foreground mt-1">Source: {agentData.repository.source}</p>
                )}
              </section>
            )}

            {agentData.image && (
              <section>
                <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-2">Container Image</h3>
                <div className="bg-muted p-3 rounded-md">
                  <code className="text-xs break-all">{agentData.image}</code>
                </div>
              </section>
            )}

            <section>
              <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-2">Timestamps</h3>
              <div className="space-y-2 text-sm">
                {agentData.updatedAt && (
                  <div className="flex justify-between text-xs">
                    <span className="text-muted-foreground">Last Updated</span>
                    <span className="font-mono">{formatDate(agentData.updatedAt)}</span>
                  </div>
                )}
                {official?.publishedAt && (
                  <div className="flex justify-between text-xs">
                    <span className="text-muted-foreground">Published</span>
                    <span className="font-mono">{formatDate(official.publishedAt)}</span>
                  </div>
                )}
                {official?.updatedAt && (
                  <div className="flex justify-between text-xs">
                    <span className="text-muted-foreground">Registry Updated</span>
                    <span className="font-mono">{formatDate(official.updatedAt)}</span>
                  </div>
                )}
              </div>
            </section>
          </TabsContent>

          <TabsContent value="raw">
            <div className="rounded-lg border p-4">
              <h3 className="text-sm font-semibold flex items-center gap-2 mb-3">
                <Code className="h-4 w-4" />
                Raw JSON
              </h3>
              <pre className="bg-muted p-3 rounded-md overflow-x-auto text-xs leading-relaxed">
                {JSON.stringify(selectedVersion, null, 2)}
              </pre>
            </div>
          </TabsContent>
        </Tabs>
    </div>
  )
}
