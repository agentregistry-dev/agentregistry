"use client"

import { PromptResponse } from "@/lib/admin-api"
import {
  TooltipProvider,
} from "@/components/ui/tooltip"
import { FileText } from "lucide-react"

interface PromptCardProps {
  prompt: PromptResponse
  onClick?: () => void
  versionCount?: number
}

export function PromptCard({ prompt, onClick, versionCount }: PromptCardProps) {
  const { prompt: promptData, _meta } = prompt
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
          <FileText className="h-4 w-4 text-primary" />
        </div>

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-0.5">
            <h3 className="text-lg font-semibold truncate">{promptData.name}</h3>
          </div>

          {promptData.description && (
            <p className="text-[15px] text-muted-foreground line-clamp-1 mb-2">
              {promptData.description}
            </p>
          )}

          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted-foreground">
            <span className="font-mono">{promptData.version}</span>
            {versionCount && versionCount > 1 && (
              <span className="text-primary text-xs">+{versionCount - 1}</span>
            )}

            {official?.publishedAt && (
              <span>{formatDate(official.publishedAt)}</span>
            )}
          </div>
        </div>
      </div>
    </TooltipProvider>
  )
}
