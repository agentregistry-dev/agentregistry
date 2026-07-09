"use client"

import type { Plugin } from "@/lib/api/types.gen"
import { getPinnedRevision, getPluginRepoUrl, getPluginResolution } from "@/lib/plugin-utils"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { AlertCircle, Blocks, CheckCircle2, Github, GitCommitHorizontal, Loader2, Trash2 } from "lucide-react"

interface PluginCardProps {
  plugin: Plugin
  onDelete?: (plugin: Plugin) => void
  showDelete?: boolean
  showExternalLinks?: boolean
  onClick?: () => void
  tagCount?: number
}

function ResolutionBadge({ plugin }: { plugin: Plugin }) {
  const resolution = getPluginResolution(plugin)

  switch (resolution.state) {
    case "ready":
      return (
        <Badge variant="outline" className="gap-1 text-xs font-normal text-green-700 dark:text-green-400 border-green-600/30">
          <CheckCircle2 className="h-3 w-3" /> Ready
        </Badge>
      )
    case "progressing":
      return (
        <Badge variant="outline" className="gap-1 text-xs font-normal text-amber-700 dark:text-amber-400 border-amber-600/30">
          <Loader2 className="h-3 w-3 animate-spin" /> Resolving
        </Badge>
      )
    case "failed":
      return (
        <Tooltip>
          <TooltipTrigger asChild>
            <Badge variant="outline" className="gap-1 text-xs font-normal text-destructive border-destructive/30">
              <AlertCircle className="h-3 w-3" /> {resolution.label}
            </Badge>
          </TooltipTrigger>
          {resolution.message && (
            <TooltipContent className="max-w-xs">{resolution.message}</TooltipContent>
          )}
        </Tooltip>
      )
    default:
      return (
        <Badge variant="outline" className="gap-1 text-xs font-normal text-muted-foreground">
          Not resolved
        </Badge>
      )
  }
}

export function PluginCard({ plugin, onDelete, showDelete = false, showExternalLinks = true, onClick, tagCount }: PluginCardProps) {
  const { metadata, spec } = plugin
  const pinnedRevision = getPinnedRevision(plugin)
  const repoUrl = getPluginRepoUrl(plugin)

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
          <Blocks className="h-4 w-4 text-primary" />
        </div>

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-0.5">
            <h3 className="text-lg font-semibold truncate">{spec.title || metadata.name}</h3>
            <ResolutionBadge plugin={plugin} />
          </div>

          <p className="text-[15px] text-muted-foreground line-clamp-1 mb-2">
            {spec.description}
          </p>

          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted-foreground">
            {metadata.tag && <span className="font-mono">{metadata.tag}</span>}
            {tagCount && tagCount > 1 && (
              <span className="text-primary text-xs">+{tagCount - 1}</span>
            )}

            {pinnedRevision && (
              <span className="flex items-center gap-1 font-mono text-xs">
                <GitCommitHorizontal className="h-3.5 w-3.5" />
                {pinnedRevision}
              </span>
            )}

            {spec.harnesses?.map((harness) => (
              <Badge key={harness} variant="secondary" className="text-[10px] px-1.5 py-0 h-4 font-normal">
                {harness}
              </Badge>
            ))}

            {metadata.createdAt && (
              <span>{formatDate(metadata.createdAt)}</span>
            )}
          </div>
        </div>

        <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity shrink-0">
          {showExternalLinks && repoUrl && (
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7"
              onClick={(e) => { e.stopPropagation(); window.open(repoUrl, '_blank') }}
            >
              <Github className="h-3.5 w-3.5" />
            </Button>
          )}
          {showDelete && onDelete && (
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7 text-destructive hover:text-destructive hover:bg-destructive/10"
              onClick={(e) => { e.stopPropagation(); onDelete(plugin) }}
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          )}
        </div>
      </div>
    </TooltipProvider>
  )
}
