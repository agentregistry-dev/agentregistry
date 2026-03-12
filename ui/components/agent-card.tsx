"use client"

import { AgentResponse } from "@/lib/admin-api"
import { Badge } from "@/components/ui/badge"
import {
  TooltipProvider,
} from "@/components/ui/tooltip"
import { Bot, Brain, Cpu, Github } from "lucide-react"

interface AgentCardProps {
  agent: AgentResponse
  onDelete?: (agent: AgentResponse) => void
  showDelete?: boolean
  showExternalLinks?: boolean
  onClick?: () => void
  versionCount?: number
}

export function AgentCard({ agent, onClick, versionCount }: AgentCardProps) {
  const { agent: agentData, _meta } = agent
  const official = _meta?.['io.modelcontextprotocol.registry/official']

  const formatDate = (dateString: string) => {
    try {
      return new Date(dateString).toLocaleDateString('en-US', {
        year: 'numeric',
        month: 'short',
        day: 'numeric',
      })
    } catch {
      return dateString
    }
  }

  return (
    <TooltipProvider>
      <div
        className="group flex items-start gap-3.5 py-4 px-2 -mx-2 rounded-md cursor-pointer transition-colors hover:bg-muted/50"
        onClick={() => onClick?.()}
      >
        <div className="w-10 h-10 rounded bg-primary/8 flex items-center justify-center flex-shrink-0 mt-0.5">
          <Bot className="h-4 w-4 text-primary" />
        </div>

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-0.5">
            <h3 className="text-lg font-semibold truncate">{agentData.name}</h3>
            <Badge variant="outline" className="text-[13px] px-2 py-0.5 font-normal">
              {agentData.framework}
            </Badge>
            <Badge variant="secondary" className="text-[13px] px-2 py-0.5 font-normal">
              {agentData.language}
            </Badge>
          </div>

          {agentData.description && (
            <p className="text-[15px] text-muted-foreground line-clamp-1 mb-2">
              {agentData.description}
            </p>
          )}

          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted-foreground">
            <span className="font-mono">{agentData.version}</span>
            {versionCount && versionCount > 1 && (
              <span className="text-primary text-xs">+{versionCount - 1}</span>
            )}

            {official?.publishedAt && (
              <span>{formatDate(official.publishedAt)}</span>
            )}

            {agentData.modelProvider && (
              <span className="flex items-center gap-1">
                <Brain className="h-3 w-3" />
                {agentData.modelProvider}
              </span>
            )}

            {agentData.modelName && (
              <span className="flex items-center gap-1 font-mono">
                <Cpu className="h-3 w-3" />
                {agentData.modelName}
              </span>
            )}

            {agentData.repository?.url && (
              <a
                href={agentData.repository.url}
                target="_blank"
                rel="noopener noreferrer"
                className="flex items-center gap-1 hover:text-primary transition-colors"
                onClick={(e) => e.stopPropagation()}
              >
                <Github className="h-3 w-3" />
                Repo
              </a>
            )}
          </div>
        </div>
      </div>
    </TooltipProvider>
  )
}
