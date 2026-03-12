"use client"

import { SkillResponse } from "@/lib/admin-api"
import { Button } from "@/components/ui/button"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { Package, ExternalLink, GitBranch, Github, Globe, Trash2, Zap } from "lucide-react"

interface SkillCardProps {
  skill: SkillResponse
  onDelete?: (skill: SkillResponse) => void
  showDelete?: boolean
  showExternalLinks?: boolean
  onClick?: () => void
  versionCount?: number
}

export function SkillCard({ skill, onDelete, showDelete = false, showExternalLinks = true, onClick, versionCount }: SkillCardProps) {
  const { skill: skillData, _meta } = skill
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
          <Zap className="h-4 w-4 text-primary" />
        </div>

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-0.5">
            <h3 className="text-lg font-semibold truncate">{skillData.title || skillData.name}</h3>
          </div>

          <p className="text-[15px] text-muted-foreground line-clamp-1 mb-2">
            {skillData.description}
          </p>

          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted-foreground">
            <span className="font-mono">{skillData.version}</span>
            {versionCount && versionCount > 1 && (
              <span className="text-primary text-xs">+{versionCount - 1}</span>
            )}

            {official?.publishedAt && (
              <span>{formatDate(official.publishedAt)}</span>
            )}

            {skillData.packages && skillData.packages.length > 0 && (
              <span className="flex items-center gap-1">
                <Package className="h-3 w-3" />
                {skillData.packages.length}
              </span>
            )}

            {skillData.remotes && skillData.remotes.length > 0 && (
              <span className="flex items-center gap-1">
                <ExternalLink className="h-3 w-3" />
                {skillData.remotes.length}
              </span>
            )}

            {skillData.repository?.source && (
              <span className="flex items-center gap-1">
                <GitBranch className="h-3 w-3" />
                {skillData.repository.source}
              </span>
            )}
          </div>
        </div>

        <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity shrink-0">
          {showExternalLinks && skillData.repository?.url && (
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7"
              onClick={(e) => { e.stopPropagation(); window.open(skillData.repository?.url || '', '_blank') }}
            >
              <Github className="h-3.5 w-3.5" />
            </Button>
          )}
          {showExternalLinks && skillData.websiteUrl && (
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7"
              onClick={(e) => { e.stopPropagation(); window.open(skillData.websiteUrl, '_blank') }}
            >
              <Globe className="h-3.5 w-3.5" />
            </Button>
          )}
          {showDelete && onDelete && (
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7 text-destructive hover:text-destructive hover:bg-destructive/10"
              onClick={(e) => { e.stopPropagation(); onDelete(skill) }}
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          )}
        </div>
      </div>
    </TooltipProvider>
  )
}
