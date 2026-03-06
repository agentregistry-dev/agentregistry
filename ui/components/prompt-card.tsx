"use client"

import { PromptResponse } from "@/lib/admin-api"
import { Card } from "@/components/ui/card"
import {
  TooltipProvider,
} from "@/components/ui/tooltip"
import { Calendar, Tag, FileText } from "lucide-react"

interface PromptCardProps {
  prompt: PromptResponse
  onClick?: () => void
}

export function PromptCard({ prompt, onClick }: PromptCardProps) {
  const { prompt: promptData, _meta } = prompt
  const official = _meta?.['io.modelcontextprotocol.registry/official']

  const handleClick = () => {
    if (onClick) {
      onClick()
    }
  }

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
          <div className="w-10 h-10 rounded bg-primary/15 flex items-center justify-center flex-shrink-0 mt-1">
            <FileText className="h-5 w-5 text-primary" />
          </div>
          <div className="flex-1 min-w-0">
            <h3 className="font-semibold text-lg mb-1">{promptData.name}</h3>
            {promptData.description && (
              <p className="text-sm text-muted-foreground line-clamp-2">
                {promptData.description}
              </p>
            )}
          </div>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground mt-3">
        <div className="flex items-center gap-1">
          <Tag className="h-3 w-3" />
          <span>{promptData.version}</span>
        </div>

        {official?.publishedAt && (
          <div className="flex items-center gap-1">
            <Calendar className="h-3 w-3" />
            <span>{formatDate(official.publishedAt)}</span>
          </div>
        )}
      </div>
      </Card>
    </TooltipProvider>
  )
}
