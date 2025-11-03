"use client"

import { ServerResponse } from "@/lib/admin-api"
import { Card } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Package, Calendar, Tag, ExternalLink, GitBranch, Star } from "lucide-react"

interface ServerCardProps {
  server: ServerResponse
  onClick: (server: ServerResponse) => void
}

export function ServerCard({ server, onClick }: ServerCardProps) {
  const { server: serverData, _meta } = server
  const official = _meta?.['io.modelcontextprotocol.registry/official']
  
  // Extract GitHub stars from metadata
  const publisherMetadata = serverData._meta?.['io.modelcontextprotocol.registry/publisher-provided']?.['agentregistry.solo.io/metadata']
  const githubStars = publisherMetadata?.stars

  // Get status badge color
  const getStatusColor = (status: string) => {
    switch (status) {
      case 'active':
        return 'bg-green-600'
      case 'deprecated':
        return 'bg-yellow-600'
      case 'deleted':
        return 'bg-red-600'
      default:
        return 'bg-gray-600'
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
      className="p-4 hover:shadow-lg transition-shadow cursor-pointer"
      onClick={() => onClick(server)}
    >
      <div className="flex items-start justify-between mb-2">
        <div className="flex-1">
          <div className="flex items-center gap-2 mb-1">
            <h3 className="font-semibold text-lg">{serverData.title || serverData.name}</h3>
            {official?.isLatest && (
              <Badge variant="default" className="text-xs">
                Latest
              </Badge>
            )}
            {official?.status && (
              <Badge variant="secondary" className={`text-xs ${getStatusColor(official.status)}`}>
                {official.status}
              </Badge>
            )}
          </div>
          <p className="text-sm text-muted-foreground">{serverData.name}</p>
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

