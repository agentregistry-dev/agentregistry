"use client"

import { AgentResponse } from "@/lib/admin-api"
import { Card } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Package, Calendar, Tag, ExternalLink, GitBranch, Github, Globe, Trash2, Bot } from "lucide-react"

interface AgentCardProps {
  agent: AgentResponse
  onDelete?: (agent: AgentResponse) => void
  showDelete?: boolean
  showExternalLinks?: boolean
  onClick?: () => void
}

export function AgentCard({ agent, onDelete, showDelete = false, showExternalLinks = true, onClick }: AgentCardProps) {
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
            <h3 className="font-semibold text-lg mb-1">{agentData.title || agentData.name}</h3>
            <p className="text-sm text-muted-foreground">{agentData.name}</p>
          </div>
        </div>
        <div className="flex items-center gap-1 ml-2">
          {showExternalLinks && agentData.repository?.url && (
            <Button
              variant="ghost"
              size="icon"
              className="h-8 w-8"
              onClick={(e) => {
                e.stopPropagation()
                window.open(agentData.repository?.url || '', '_blank')
              }}
              title="View on GitHub"
            >
              <Github className="h-4 w-4" />
            </Button>
          )}
          {showExternalLinks && agentData.websiteUrl && (
            <Button
              variant="ghost"
              size="icon"
              className="h-8 w-8"
              onClick={(e) => {
                e.stopPropagation()
                window.open(agentData.websiteUrl, '_blank')
              }}
              title="Visit website"
            >
              <Globe className="h-4 w-4" />
            </Button>
          )}
          {showDelete && onDelete && (
            <Button
              variant="ghost"
              size="icon"
              className="h-8 w-8 text-destructive hover:text-destructive hover:bg-destructive/10"
              onClick={(e) => {
                e.stopPropagation()
                onDelete(agent)
              }}
              title="Remove from registry"
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          )}
        </div>
      </div>

      <p className="text-sm text-muted-foreground mb-3 line-clamp-2">
        {agentData.description}
      </p>

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

        {agentData.packages && agentData.packages.length > 0 && (
          <div className="flex items-center gap-1">
            <Package className="h-3 w-3" />
            <span>{agentData.packages.length} package{agentData.packages.length !== 1 ? 's' : ''}</span>
          </div>
        )}

        {agentData.remotes && agentData.remotes.length > 0 && (
          <div className="flex items-center gap-1">
            <ExternalLink className="h-3 w-3" />
            <span>{agentData.remotes.length} remote{agentData.remotes.length !== 1 ? 's' : ''}</span>
          </div>
        )}

        {agentData.repository && (
          <div className="flex items-center gap-1">
            <GitBranch className="h-3 w-3" />
            <span>{agentData.repository.source}</span>
          </div>
        )}
      </div>
    </Card>
  )
}

