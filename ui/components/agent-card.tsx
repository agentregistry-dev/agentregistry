"use client"

import { AgentResponse } from "@/lib/admin-api"
import { Card } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { Calendar, Tag, Bot, Upload, Container, Cpu, Brain, Github } from "lucide-react"

interface AgentCardProps {
  agent: AgentResponse
  onDelete?: (agent: AgentResponse) => void
  onPublish?: (agent: AgentResponse) => void
  showDelete?: boolean
  showPublish?: boolean
  showExternalLinks?: boolean
  onClick?: () => void
}

export function AgentCard({ agent, onDelete, onPublish, showDelete = false, showPublish = false, onClick }: AgentCardProps) {
  const { agent: agentData, _meta } = agent
  const official = _meta?.['io.modelcontextprotocol.registry/official']

  const handleClick = () => {
    if (onClick) {
      onClick()
    }
  }

  // Format date
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
      <Card
        className="p-4 hover:shadow-md transition-all duration-200 cursor-pointer border hover:border-primary/20"
        onClick={handleClick}
      >
      <div className="flex items-start justify-between mb-2">
        <div className="flex items-start gap-3 flex-1">
          <div className="w-10 h-10 rounded bg-primary/20 flex items-center justify-center flex-shrink-0 mt-1">
            <Bot className="h-5 w-5 text-primary" />
          </div>
          <div className="flex-1 min-w-0">
            <h3 className="font-semibold text-lg mb-1">{agentData.name}</h3>
            <p className="text-xs text-muted-foreground flex items-center gap-1 flex-wrap">
              <Badge variant="outline" className="text-xs">
                {agentData.framework}
              </Badge>
              <Badge variant="secondary" className="text-xs">
                {agentData.language}
              </Badge>
            </p>
          </div>
        </div>
        <div className="flex items-center gap-1 ml-2">
          {showPublish && onPublish && (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-8 w-8"
                  onClick={(e) => {
                    e.stopPropagation()
                    onPublish(agent)
                  }}
                >
                  <Upload className="h-4 w-4" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                <p>Publish this agent to your registry</p>
              </TooltipContent>
            </Tooltip>
          )}
        </div>
      </div>

      {agentData.description && (
        <p className="text-sm text-muted-foreground mb-3 line-clamp-2">
          {agentData.description}
        </p>
      )}

      <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
        <div className="flex items-center gap-1">
          <Tag className="h-3 w-3" />
          <span>{agentData.version}</span>
        </div>

        {official?.publishedAt && (
          <div className="flex items-center gap-1">
            <Calendar className="h-3 w-3" />
            <span>{formatDate(official.publishedAt)}</span>
          </div>
        )}

        {agentData.modelProvider && (
          <div className="flex items-center gap-1">
            <Brain className="h-3 w-3" />
            <span>{agentData.modelProvider}</span>
          </div>
        )}

        {agentData.modelName && (
          <div className="flex items-center gap-1">
            <Cpu className="h-3 w-3" />
            <span className="font-mono text-xs">{agentData.modelName}</span>
          </div>
        )}

        {agentData.image && (
          <div className="flex items-center gap-1">
            <Container className="h-3 w-3" />
            <span className="font-mono text-xs truncate max-w-[200px]" title={agentData.image}>
              {agentData.image}
            </span>
          </div>
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
            <span>Repository</span>
          </a>
        )}
      </div>
      </Card>
    </TooltipProvider>
  )
}

