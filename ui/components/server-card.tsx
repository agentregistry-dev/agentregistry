"use client"

import { ServerResponse } from "@/lib/admin-api"
import { Card } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { Package, Calendar, Tag, ExternalLink, GitBranch, Star, Github, Globe, Trash2, Upload, ShieldCheck, BadgeCheck, Play } from "lucide-react"

interface ServerCardProps {
  server: ServerResponse
  onDelete?: (server: ServerResponse) => void
  onPublish?: (server: ServerResponse) => void
  onDeploy?: (server: ServerResponse) => void
  showDelete?: boolean
  showPublish?: boolean
  showDeploy?: boolean
  showExternalLinks?: boolean
  onClick?: () => void
  versionCount?: number
}

export function ServerCard({ server, onDelete, onPublish, onDeploy, showDelete = false, showPublish = false, showDeploy = false, showExternalLinks = true, onClick, versionCount }: ServerCardProps) {
  const { server: serverData, _meta } = server
  const official = _meta?.['io.modelcontextprotocol.registry/official']
  
  // Extract metadata
  const publisherMetadata = serverData._meta?.['io.modelcontextprotocol.registry/publisher-provided']?.['aregistry.ai/metadata']
  const githubStars = publisherMetadata?.stars
  const identityData = publisherMetadata?.identity

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
    <TooltipProvider>
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
            <div className="flex items-center gap-2 mb-1">
              <h3 className="font-semibold text-lg">{serverData.title || serverData.name}</h3>
              <Tooltip>
                <TooltipTrigger asChild>
                  <ShieldCheck 
                    className={`h-4 w-4 flex-shrink-0 ${
                      identityData?.org_is_verified 
                        ? 'text-blue-600 dark:text-blue-400' 
                        : 'text-gray-400 dark:text-gray-600 opacity-40'
                    }`}
                  />
                </TooltipTrigger>
                <TooltipContent>
                  <p>{identityData?.org_is_verified ? 'Verified Organization' : 'Organization Not Verified'}</p>
                </TooltipContent>
              </Tooltip>
              <Tooltip>
                <TooltipTrigger asChild>
                  <BadgeCheck 
                    className={`h-4 w-4 flex-shrink-0 ${
                      identityData?.publisher_identity_verified_by_jwt 
                        ? 'text-green-600 dark:text-green-400' 
                        : 'text-gray-400 dark:text-gray-600 opacity-40'
                    }`}
                  />
                </TooltipTrigger>
                <TooltipContent>
                  <p>{identityData?.publisher_identity_verified_by_jwt ? 'Verified Publisher' : 'Publisher Not Verified'}</p>
                </TooltipContent>
              </Tooltip>
            </div>
            <p className="text-sm text-muted-foreground">{serverData.name}</p>
          </div>
        </div>
        <div className="flex items-center gap-1 ml-2">
          {showDeploy && onDeploy && (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="default"
                  size="sm"
                  className="h-8 gap-1.5"
                  onClick={(e) => {
                    e.stopPropagation()
                    onDeploy(server)
                  }}
                >
                  <Play className="h-3.5 w-3.5" />
                  Deploy
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                <p>Deploy this server</p>
              </TooltipContent>
            </Tooltip>
          )}
          {showPublish && onPublish && (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-8 w-8"
                  onClick={(e) => {
                    e.stopPropagation()
                    onPublish(server)
                  }}
                >
                  <Upload className="h-4 w-4" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                <p>Publish this server to your registry</p>
              </TooltipContent>
            </Tooltip>
          )}
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
          {versionCount && versionCount > 1 && (
            <span className="ml-1 text-primary font-medium">
              (+{versionCount - 1} more)
            </span>
          )}
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
    </TooltipProvider>
  )
}

