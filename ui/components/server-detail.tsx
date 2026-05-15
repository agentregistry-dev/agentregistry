"use client"

import { useState } from "react"
import { ServerResponse } from "@/lib/admin-api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { RuntimeArgumentsTable } from "@/components/server-detail/runtime-arguments-table"
import { EnvironmentVariablesTable } from "@/components/server-detail/environment-variables-table"
import {
  Package,
  Calendar,
  ExternalLink,
  Code,
  Star,
  Copy,
  History,
  Check,
  GitFork,
  Eye,
  ShieldCheck,
  BadgeCheck,
} from "lucide-react"

interface ServerDetailProps {
  server: ServerResponse & { allTags?: ServerResponse[] }
  onServerCopied?: () => void
}

export function ServerDetail({ server, onServerCopied }: ServerDetailProps) {
  const [activeTab, setActiveTab] = useState("overview")
  const [selectedTag, setSelectedTag] = useState<ServerResponse>(server)
  const [jsonCopied, setJsonCopied] = useState(false)

  const allTags = server.allTags || [server]

  const { server: serverData, _meta } = selectedTag
  const official = _meta?.['io.modelcontextprotocol.registry/official']

  const publisherProvided = serverData._meta?.['io.modelcontextprotocol.registry/publisher-provided'] as Record<string, unknown> | undefined
  const publisherMetadata = publisherProvided?.['aregistry.ai/metadata'] as Record<string, any> | undefined
  const githubStars = publisherMetadata?.stars as number | undefined
  const repoData = publisherMetadata?.repo as Record<string, any> | undefined
  const identityData = publisherMetadata?.identity as Record<string, any> | undefined

  const handleTagChange = (tag: string) => {
    const newTag = allTags.find(v => v.server.tag === tag)
    if (newTag) setSelectedTag(newTag)
  }

  const handleCopyJson = async () => {
    try {
      await navigator.clipboard.writeText(JSON.stringify(selectedTag, null, 2))
      setJsonCopied(true)
      setTimeout(() => setJsonCopied(false), 2000)
    } catch (err) {
      console.error('Failed to copy JSON:', err)
    }
  }

  const formatDate = (dateString: string) => {
    try {
      return new Date(dateString).toLocaleString('en-US', {
        year: 'numeric',
        month: 'long',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
      })
    } catch {
      return dateString
    }
  }

  return (
    <TooltipProvider>
      <div className="space-y-6">
          {/* Header */}
          <div className="flex items-start gap-4">
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2 mb-1">
                <h1 className="text-2xl font-bold truncate">{serverData.title || serverData.name}</h1>
                {identityData?.org_is_verified && (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <ShieldCheck className="h-5 w-5 text-blue-500 flex-shrink-0" />
                    </TooltipTrigger>
                    <TooltipContent><p>Verified Organization</p></TooltipContent>
                  </Tooltip>
                )}
                {identityData?.publisher_identity_verified_by_jwt && (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <BadgeCheck className="h-5 w-5 text-green-500 flex-shrink-0" />
                    </TooltipTrigger>
                    <TooltipContent><p>Verified Publisher</p></TooltipContent>
                  </Tooltip>
                )}
              </div>
              <p className="text-[15px] text-muted-foreground">{serverData.name}</p>
            </div>
          </div>

          {/* Tag selector */}
          {allTags.length > 1 && (
            <div className="flex items-center gap-3 px-3 py-2 bg-accent/50 border border-primary/10 rounded-md">
              <History className="h-4 w-4 text-muted-foreground" />
              <span className="text-sm">{allTags.length} tags</span>
              <Select value={selectedTag.server.tag} onValueChange={handleTagChange}>
                <SelectTrigger className="w-[160px] h-7 text-sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {allTags.map((tag) => (
                    <SelectItem key={tag.server.tag} value={tag.server.tag}>
                      {tag.server.tag}
                      {tag.server.tag === server.server.tag && " (latest)"}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}

          {/* Quick info pills */}
          <div className="flex flex-wrap gap-2 text-sm">
            <span className="flex items-center gap-1.5 px-2.5 py-1 bg-muted rounded text-sm">
              <span className="font-mono">{serverData.tag}</span>
              {allTags.length > 1 && (
                <Badge variant="secondary" className="text-[10px] px-1 py-0 h-3.5">{allTags.length} total</Badge>
              )}
            </span>
            {official?.publishedAt && (
              <span className="flex items-center gap-1.5 px-2.5 py-1 bg-muted rounded text-sm">
                <Calendar className="h-3 w-3 text-muted-foreground" />
                {formatDate(official.publishedAt)}
              </span>
            )}
          </div>

          {/* Tabs */}
          <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full">
            <TabsList className="mb-4">
              <TabsTrigger value="overview">Overview</TabsTrigger>
              {serverData.source?.package && (
                <TabsTrigger value="packages">Package</TabsTrigger>
              )}
              <TabsTrigger value="raw">Raw</TabsTrigger>
            </TabsList>

            <TabsContent value="overview" className="space-y-6">
              <section>
                <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-2">Description</h3>
                <p className="text-[15px] leading-relaxed">{serverData.description}</p>
              </section>

              {(() => {
                const repoUrl = serverData.source?.repository?.url
                if (!repoUrl) return null
                return (
                  <section>
                    <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-2">Repository</h3>
                    <div className="space-y-2 text-sm">
                      <div className="flex items-center justify-between">
                        <span className="text-muted-foreground">URL</span>
                        <a
                          href={repoUrl}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="text-sm text-primary hover:underline flex items-center gap-1"
                        >
                          {repoUrl} <ExternalLink className="h-3 w-3" />
                        </a>
                      </div>
                    </div>
                  </section>
                )
              })()}

              {/* Repo stats */}
              {(githubStars !== undefined || repoData) && (
                <section>
                  <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-3">Repository Stats</h3>
                  <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
                    {githubStars !== undefined && (
                      <div className="flex items-center gap-2 p-3 rounded-md bg-muted/50">
                        <Star className="h-4 w-4 text-amber-500 fill-amber-500" />
                        <div>
                          <p className="text-[10px] text-muted-foreground">Stars</p>
                          <p className="text-lg font-bold">{githubStars.toLocaleString()}</p>
                        </div>
                      </div>
                    )}
                    {repoData?.forks_count !== undefined && (
                      <div className="flex items-center gap-2 p-3 rounded-md bg-muted/50">
                        <GitFork className="h-4 w-4 text-muted-foreground" />
                        <div>
                          <p className="text-[10px] text-muted-foreground">Forks</p>
                          <p className="text-lg font-bold">{repoData.forks_count.toLocaleString()}</p>
                        </div>
                      </div>
                    )}
                    {repoData?.watchers_count !== undefined && (
                      <div className="flex items-center gap-2 p-3 rounded-md bg-muted/50">
                        <Eye className="h-4 w-4 text-muted-foreground" />
                        <div>
                          <p className="text-[10px] text-muted-foreground">Watchers</p>
                          <p className="text-lg font-bold">{repoData.watchers_count.toLocaleString()}</p>
                        </div>
                      </div>
                    )}
                    {repoData?.primary_language && (
                      <div className="flex items-center gap-2 p-3 rounded-md bg-muted/50">
                        <Code className="h-4 w-4 text-muted-foreground" />
                        <div>
                          <p className="text-[10px] text-muted-foreground">Language</p>
                          <p className="text-sm font-bold">{repoData.primary_language}</p>
                        </div>
                      </div>
                    )}
                  </div>
                  {(() => {
                    const repoUrl = serverData.source?.repository?.url
                    if (!repoUrl) return null
                    return (
                      <a
                        href={repoUrl}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="flex items-center gap-1.5 text-xs text-primary hover:underline mt-3"
                      >
                        <ExternalLink className="h-3 w-3" />
                        View Repository
                      </a>
                    )
                  })()}
                </section>
              )}
            </TabsContent>

            <TabsContent value="packages" className="space-y-4">
              {(() => {
                const pkg = serverData.source?.package
                if (!pkg) {
                  return <p className="text-center text-sm text-muted-foreground py-8">No package defined</p>
                }
                return (
                    <div className="p-4 rounded-lg border">
                      <div className="flex items-center justify-between mb-3">
                        <div className="flex items-center gap-2">
                          <Package className="h-4 w-4 text-primary" />
                          <h4 className="text-sm font-semibold">{pkg.identifier}</h4>
                        </div>
                        <Badge variant="outline" className="text-xs">{pkg.registryType}</Badge>
                      </div>
                      <div className="space-y-1.5 text-sm mb-3 pb-3 border-b">
                        <div className="flex justify-between text-xs">
                          <span className="text-muted-foreground">Version</span>
                          <span className="font-mono">{pkg.version}</span>
                        </div>
                        {(pkg as any).runtimeHint && (
                          <div className="flex justify-between text-xs">
                            <span className="text-muted-foreground">Runtime</span>
                            <Badge variant="secondary" className="text-[10px] h-4">{(pkg as any).runtimeHint}</Badge>
                          </div>
                        )}
                        {(pkg as any).transport?.type && (
                          <div className="flex justify-between text-xs">
                            <span className="text-muted-foreground">Transport</span>
                            <Badge variant="secondary" className="text-[10px] h-4">{(pkg as any).transport.type}</Badge>
                          </div>
                        )}
                      </div>
                      <RuntimeArgumentsTable arguments={(pkg as any).runtimeArguments} />
                      <EnvironmentVariablesTable variables={(pkg as any).environmentVariables} />
                    </div>
                )
              })()}
            </TabsContent>

            <TabsContent value="raw">
              <div className="rounded-lg border p-4">
                <div className="flex items-center justify-between mb-3">
                  <h3 className="text-sm font-semibold flex items-center gap-2">
                    <Code className="h-4 w-4" />
                    Raw JSON
                  </h3>
                  <Button variant="outline" size="sm" onClick={handleCopyJson} className="gap-1.5 h-7 text-xs">
                    {jsonCopied ? <><Check className="h-3 w-3" /> Copied</> : <><Copy className="h-3 w-3" /> Copy</>}
                  </Button>
                </div>
                <pre className="bg-muted p-3 rounded-md overflow-x-auto text-xs leading-relaxed">
                  {JSON.stringify(selectedTag, null, 2)}
                </pre>
              </div>
            </TabsContent>
          </Tabs>
      </div>
    </TooltipProvider>
  )
}
