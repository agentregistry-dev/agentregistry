"use client"

import { SkillResponse } from "@/lib/admin-api"
import { Card } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Package, Calendar, Tag, ExternalLink, GitBranch, Github, Globe, Trash2, Zap } from "lucide-react"

interface SkillCardProps {
  skill: SkillResponse
  onDelete?: (skill: SkillResponse) => void
  showDelete?: boolean
  showExternalLinks?: boolean
  onClick?: () => void
}

export function SkillCard({ skill, onDelete, showDelete = false, showExternalLinks = true, onClick }: SkillCardProps) {
  const { skill: skillData, _meta } = skill
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
          <div className="w-10 h-10 rounded bg-primary/10 flex items-center justify-center flex-shrink-0 mt-1">
            <Zap className="h-5 w-5 text-primary" />
          </div>
          <div className="flex-1 min-w-0">
            <h3 className="font-semibold text-lg mb-1">{skillData.title || skillData.name}</h3>
            <p className="text-sm text-muted-foreground">{skillData.name}</p>
          </div>
        </div>
        <div className="flex items-center gap-1 ml-2">
          {showExternalLinks && skillData.repository?.url && (
            <Button
              variant="ghost"
              size="icon"
              className="h-8 w-8"
              onClick={(e) => {
                e.stopPropagation()
                window.open(skillData.repository?.url || '', '_blank')
              }}
              title="View on GitHub"
            >
              <Github className="h-4 w-4" />
            </Button>
          )}
          {showExternalLinks && skillData.websiteUrl && (
            <Button
              variant="ghost"
              size="icon"
              className="h-8 w-8"
              onClick={(e) => {
                e.stopPropagation()
                window.open(skillData.websiteUrl, '_blank')
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
                onDelete(skill)
              }}
              title="Remove from registry"
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          )}
        </div>
      </div>

      <p className="text-sm text-muted-foreground mb-3 line-clamp-2">
        {skillData.description}
      </p>

      <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
        <div className="flex items-center gap-1">
          <Tag className="h-3 w-3" />
          <span>{skillData.version}</span>
        </div>

        {official?.publishedAt && (
          <div className="flex items-center gap-1">
            <Calendar className="h-3 w-3" />
            <span>{formatDate(official.publishedAt)}</span>
          </div>
        )}

        {skillData.packages && skillData.packages.length > 0 && (
          <div className="flex items-center gap-1">
            <Package className="h-3 w-3" />
            <span>{skillData.packages.length} package{skillData.packages.length !== 1 ? 's' : ''}</span>
          </div>
        )}

        {skillData.remotes && skillData.remotes.length > 0 && (
          <div className="flex items-center gap-1">
            <ExternalLink className="h-3 w-3" />
            <span>{skillData.remotes.length} remote{skillData.remotes.length !== 1 ? 's' : ''}</span>
          </div>
        )}

        {skillData.repository && (
          <div className="flex items-center gap-1">
            <GitBranch className="h-3 w-3" />
            <span>{skillData.repository.source}</span>
          </div>
        )}
      </div>
    </Card>
  )
}

