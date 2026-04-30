"use client"

import { ServerResponse } from "@/lib/admin-api"
import { Button } from "@/components/ui/button"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { Package, Star, Github, Trash2, ShieldCheck, BadgeCheck, Play } from "lucide-react"

interface ServerCardProps {
  server: ServerResponse
  onDelete?: (server: ServerResponse) => void
  onDeploy?: (server: ServerResponse) => void
  showDelete?: boolean
  showDeploy?: boolean
  showExternalLinks?: boolean
  onClick?: () => void
  versionCount?: number
}

export function ServerCard({ server, onDelete, onDeploy, showDelete = false, showDeploy = false, showExternalLinks = true, onClick, versionCount }: ServerCardProps) {
  const { server: serverData, _meta } = server
  const official = _meta?.['io.modelcontextprotocol.registry/official']

  const publisherProvided = serverData._meta?.['io.modelcontextprotocol.registry/publisher-provided'] as Record<string, any> | undefined
  const publisherMetadata = publisherProvided?.['aregistry.ai/metadata'] as Record<string, any> | undefined
  const githubStars = publisherMetadata?.stars
  const identityData = publisherMetadata?.identity
  const hasOciPackage = serverData.packages?.some(pkg => pkg.registryType === "oci") ?? false

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
          <span className="text-xs font-semibold text-primary uppercase">
            {serverData.name.slice(0, 2)}
          </span>
        </div>

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-0.5">
            <h3 className="text-lg font-semibold truncate">{serverData.title || serverData.name}</h3>
            {identityData?.org_is_verified && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <ShieldCheck className="h-3.5 w-3.5 text-blue-500 flex-shrink-0" />
                </TooltipTrigger>
                <TooltipContent><p>Verified Organization</p></TooltipContent>
              </Tooltip>
            )}
            {identityData?.publisher_identity_verified_by_jwt && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <BadgeCheck className="h-3.5 w-3.5 text-green-500 flex-shrink-0" />
                </TooltipTrigger>
                <TooltipContent><p>Verified Publisher</p></TooltipContent>
              </Tooltip>
            )}
          </div>

          <p className="text-[15px] text-muted-foreground line-clamp-1 mb-2">
            {serverData.description}
          </p>

          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted-foreground">
            <span className="font-mono">{serverData.version}</span>
            {versionCount && versionCount > 1 && (
              <span className="text-primary text-xs">+{versionCount - 1}</span>
            )}

            {official?.publishedAt && (
              <span>{formatDate(official.publishedAt)}</span>
            )}

            {serverData.packages && serverData.packages.length > 0 && (
              <span className="flex items-center gap-1">
                <Package className="h-3 w-3" />
                {serverData.packages.length}
              </span>
            )}

            {githubStars !== undefined && githubStars > 0 && (
              <span className="flex items-center gap-1 text-amber-500">
                <Star className="h-3 w-3 fill-amber-500" />
                {githubStars.toLocaleString()}
              </span>
            )}
          </div>
        </div>

        <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity shrink-0">
          {showDeploy && onDeploy && (
            hasOciPackage ? (
              <Button
                variant="default"
                size="sm"
                className="h-7 gap-1 text-xs"
                onClick={(e) => { e.stopPropagation(); onDeploy(server) }}
              >
                <Play className="h-3 w-3" aria-hidden="true" />
                Deploy
              </Button>
            ) : (
              <Tooltip>
                <TooltipTrigger asChild>
                  <span tabIndex={0} className="pointer-events-auto" onClick={(e) => e.stopPropagation()}>
                    <Button
                      variant="default"
                      size="sm"
                      className="h-7 gap-1 text-xs pointer-events-none"
                      disabled
                    >
                      <Play className="h-3 w-3" aria-hidden="true" />
                      Deploy
                    </Button>
                  </span>
                </TooltipTrigger>
                <TooltipContent><p>No OCI package available</p></TooltipContent>
              </Tooltip>
            )
          )}
          {showExternalLinks && serverData.repository?.url && (
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7"
              onClick={(e) => { e.stopPropagation(); window.open(serverData.repository?.url || '', '_blank') }}
              aria-label="View repository"
            >
              <Github className="h-3.5 w-3.5" aria-hidden="true" />
            </Button>
          )}
          {showDelete && onDelete && (
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7 text-destructive hover:text-destructive hover:bg-destructive/10"
              onClick={(e) => { e.stopPropagation(); onDelete(server) }}
              aria-label="Delete server"
            >
              <Trash2 className="h-3.5 w-3.5" aria-hidden="true" />
            </Button>
          )}
        </div>
      </div>
    </TooltipProvider>
  )
}
