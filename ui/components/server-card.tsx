"use client"

import { ServerResponse } from "@/lib/admin-api"
import { Card } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Package, Calendar, Tag, ExternalLink, GitBranch, Star, Github, Globe, Trash2 } from "lucide-react"

interface ServerCardProps {
  server: ServerResponse
  onDelete?: (server: ServerResponse) => void
  showDelete?: boolean
  showExternalLinks?: boolean
  onClick?: () => void
}

export function ServerCard({ server, onDelete, showDelete = false, showExternalLinks = true, onClick }: ServerCardProps) {
  const { server: serverData, _meta } = server
  const official = _meta?.['io.modelcontextprotocol.registry/official']
  
  // Extract GitHub stars from metadata
  const publisherMetadata = serverData._meta?.['io.modelcontextprotocol.registry/publisher-provided']?.['agentregistry.solo.io/metadata']
  const githubStars = publisherMetadata?.stars

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

  // Get the first icon if available
  const icon = serverData.icons?.[0]

  return (
    <Card
      className="p-4 hover:shadow-md transition-all duration-200 cursor-pointer border hover:border-primary/20"
      onClick={handleClick}
    >
      <div className="flex items-start justify-between mb-2">
        <div className="flex items-start gap-3 flex-1">
          {icon && (
            <img 
              src={icon.src} 
              alt="Server icon" 
              className="w-10 h-10 rounded flex-shrink-0 mt-1"
            />
          )}
          <div className="flex-1 min-w-0">
            <h3 className="font-semibold text-lg mb-1">{serverData.title || serverData.name}</h3>
            <p className="text-sm text-muted-foreground">{serverData.name}</p>
          </div>
        </div>
        <div className="flex items-center gap-1 ml-2">
          {showExternalLinks && serverData.repository?.url && (
            <Button
              variant="ghost"
              size="icon"
              className="h-8 w-8"
              onClick={(e) => {
                e.stopPropagation()
                window.open(serverData.repository?.url || '', '_blank')
              }}
              title="View on GitHub"
            >
              <Github className="h-4 w-4" />
            </Button>
          )}
          {showExternalLinks && serverData.websiteUrl && (
            <Button
              variant="ghost"
              size="icon"
              className="h-8 w-8"
              onClick={(e) => {
                e.stopPropagation()
                window.open(serverData.websiteUrl, '_blank')
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
                onDelete(server)
              }}
              title="Remove from registry"
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          )}
        </div>
      </div>

      <p className="text-sm text-muted-foreground mb-3 line-clamp-2">
        {serverData.description}
      </p>

      <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
        <div className="flex items-center gap-1">
          <Tag className="h-3 w-3" />
          <span>{serverData.version}</span>
        </div>

        {official?.publishedAt && (
          <div className="flex items-center gap-1">
            <Calendar className="h-3 w-3" />
            <span>{formatDate(official.publishedAt)}</span>
          </div>
        )}

        {serverData.packages && serverData.packages.length > 0 && (
          <div className="flex items-center gap-1">
            <Package className="h-3 w-3" />
            <span>{serverData.packages.length} package{serverData.packages.length !== 1 ? 's' : ''}</span>
          </div>
        )}

        {serverData.remotes && serverData.remotes.length > 0 && (
          <div className="flex items-center gap-1">
            <ExternalLink className="h-3 w-3" />
            <span>{serverData.remotes.length} remote{serverData.remotes.length !== 1 ? 's' : ''}</span>
          </div>
        )}

        {serverData.repository && (
          <div className="flex items-center gap-1">
            <GitBranch className="h-3 w-3" />
            <span>{serverData.repository.source}</span>
          </div>
        )}

        {githubStars !== undefined && (
          <div className="flex items-center gap-1 text-yellow-600 dark:text-yellow-400">
            <Star className="h-3 w-3 fill-yellow-600 dark:fill-yellow-400" />
            <span className="font-medium">{githubStars.toLocaleString()}</span>
          </div>
        )}
      </div>
    </Card>
  )
}

