"use client"

import { useState, useEffect } from "react"
import { ServerResponse, adminApiClient } from "@/lib/admin-api"
import { Card } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  X,
  Package,
  Calendar,
  Tag,
  ExternalLink,
  GitBranch,
  Globe,
  Code,
  Server,
  Link,
  Star,
  TrendingUp,
  Copy,
  Loader2,
  CheckCircle2,
  AlertCircle,
  ArrowLeft,
} from "lucide-react"

interface ServerDetailProps {
  server: ServerResponse
  onClose: () => void
  onServerCopied?: () => void
}

export function ServerDetail({ server, onClose, onServerCopied }: ServerDetailProps) {
  const [activeTab, setActiveTab] = useState("overview")
  const [copying, setCopying] = useState(false)
  const [copySuccess, setCopySuccess] = useState(false)
  const [copyError, setCopyError] = useState<string | null>(null)
  
  const { server: serverData, _meta } = server
  const official = _meta?.['io.modelcontextprotocol.registry/official']
  
  // Extract GitHub stars from metadata
  const publisherMetadata = serverData._meta?.['io.modelcontextprotocol.registry/publisher-provided']?.['agentregistry.solo.io/metadata']
  const githubStars = publisherMetadata?.stars

  // Get the first icon if available
  const icon = serverData.icons?.[0]

  // Handle ESC key to close
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        onClose()
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => {
      window.removeEventListener('keydown', handleKeyDown)
    }
  }, [onClose])

  const handleCopyToPrivateRegistry = async () => {
    setCopying(true)
    setCopyError(null)
    setCopySuccess(false)

    try {
      // Copy the server data to create a new entry
      await adminApiClient.createServer(serverData)
      setCopySuccess(true)
      
      // Notify parent component
      if (onServerCopied) {
        onServerCopied()
      }

      // Auto-hide success message after 3 seconds
      setTimeout(() => {
        setCopySuccess(false)
      }, 3000)
    } catch (err) {
      setCopyError(err instanceof Error ? err.message : "Failed to copy server")
    } finally {
      setCopying(false)
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

  return (
    <div className="fixed inset-0 bg-background z-50 overflow-y-auto">
      <div className="container mx-auto px-6 py-6">
        {/* Back Button */}
        <Button
          variant="ghost"
          onClick={onClose}
          className="mb-4 gap-2"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to Servers
        </Button>

        {/* Header */}
        <div className="flex items-center justify-between mb-6">
          <div className="flex items-start gap-4 flex-1">
            {icon && (
              <img 
                src={icon.src} 
                alt="Server icon" 
                className="w-16 h-16 rounded flex-shrink-0 mt-1"
              />
            )}
            <div className="flex-1">
              <div className="flex items-center gap-3 mb-2 flex-wrap">
                <h1 className="text-3xl font-bold">{serverData.title || serverData.name}</h1>
              </div>
              <p className="text-muted-foreground">{serverData.name}</p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              onClick={handleCopyToPrivateRegistry}
              disabled={copying}
              className="gap-2"
            >
              {copying ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <Copy className="h-4 w-4" />
              )}
              Copy to Private Registry
            </Button>
            <Button variant="ghost" size="icon" onClick={onClose}>
              <X className="h-5 w-5" />
            </Button>
          </div>
        </div>

        {/* Copy Status Messages */}
        {copySuccess && (
          <Card className="p-4 mb-4 bg-green-50 border-green-200 dark:bg-green-950 dark:border-green-800">
            <div className="flex items-center gap-2">
              <CheckCircle2 className="h-5 w-5 text-green-600 dark:text-green-400" />
              <p className="text-sm font-medium text-green-900 dark:text-green-100">
                Server successfully copied to your private registry!
              </p>
            </div>
          </Card>
        )}

        {copyError && (
          <Card className="p-4 mb-4 bg-red-50 border-red-200 dark:bg-red-950 dark:border-red-800">
            <div className="flex items-center gap-2">
              <AlertCircle className="h-5 w-5 text-red-600 dark:text-red-400" />
              <p className="text-sm font-medium text-red-900 dark:text-red-100">
                {copyError}
              </p>
            </div>
          </Card>
        )}

        {/* Quick Info */}
        <div className="flex flex-wrap gap-3 mb-6 text-sm">
          <div className="flex items-center gap-2 px-3 py-2 bg-muted rounded-md">
            <Tag className="h-3.5 w-3.5 text-muted-foreground" />
            <span className="text-muted-foreground">Version:</span>
            <span className="font-medium">{serverData.version}</span>
          </div>

          {official?.publishedAt && (
            <div className="flex items-center gap-2 px-3 py-2 bg-muted rounded-md">
              <Calendar className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-muted-foreground">Published:</span>
              <span className="font-medium">{formatDate(official.publishedAt)}</span>
            </div>
          )}

          {official?.updatedAt && (
            <div className="flex items-center gap-2 px-3 py-2 bg-muted rounded-md">
              <Calendar className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-muted-foreground">Updated:</span>
              <span className="font-medium">{formatDate(official.updatedAt)}</span>
            </div>
          )}

          {serverData.websiteUrl && (
            <a
              href={serverData.websiteUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-center gap-2 px-3 py-2 bg-muted rounded-md hover:bg-muted/80 transition-colors"
            >
              <Globe className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-muted-foreground">Website:</span>
              <span className="font-medium text-blue-600 flex items-center gap-1">
                Visit <ExternalLink className="h-3 w-3" />
              </span>
            </a>
          )}
        </div>

        {/* Detailed Information Tabs */}
        <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full">
          <TabsList>
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="score">Score</TabsTrigger>
            {serverData.packages && serverData.packages.length > 0 && (
              <TabsTrigger value="packages">Packages</TabsTrigger>
            )}
            {serverData.remotes && serverData.remotes.length > 0 && (
              <TabsTrigger value="remotes">Remotes</TabsTrigger>
            )}
            <TabsTrigger value="raw">Raw Data</TabsTrigger>
          </TabsList>

          <TabsContent value="overview" className="space-y-4">
            {/* Description */}
            <Card className="p-6">
              <h3 className="text-lg font-semibold mb-4">Description</h3>
              <p className="text-base">{serverData.description}</p>
            </Card>

            {/* Repository */}
            {serverData.repository?.url && (
              <Card className="p-6">
                <h3 className="text-lg font-semibold mb-4 flex items-center gap-2">
                  <GitBranch className="h-5 w-5" />
                  Repository
                </h3>
                <div className="space-y-2">
                  {serverData.repository.source && (
                    <div className="flex items-center justify-between">
                      <span className="text-sm text-muted-foreground">Source</span>
                      <Badge variant="outline">{serverData.repository.source}</Badge>
                    </div>
                  )}
                  <div className="flex items-center justify-between">
                    <span className="text-sm text-muted-foreground">URL</span>
                    <a
                      href={serverData.repository.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-sm text-blue-600 hover:underline flex items-center gap-1"
                    >
                      {serverData.repository.url} <ExternalLink className="h-3 w-3" />
                    </a>
                  </div>
                </div>
              </Card>
            )}
          </TabsContent>

          <TabsContent value="score" className="space-y-4">
            <Card className="p-6">
              <h3 className="text-lg font-semibold mb-6 flex items-center gap-2">
                <TrendingUp className="h-5 w-5" />
                Server Popularity Score
              </h3>
              
              {/* GitHub Stars */}
              <div className="space-y-6">
                <div className="flex items-center justify-between p-6 bg-gradient-to-r from-yellow-50 to-orange-50 dark:from-yellow-950/20 dark:to-orange-950/20 rounded-lg border border-yellow-200 dark:border-yellow-800">
                  <div className="flex items-center gap-4">
                    <div className="p-3 bg-yellow-100 dark:bg-yellow-900/30 rounded-full">
                      <Star className="h-8 w-8 text-yellow-600 dark:text-yellow-400 fill-yellow-600 dark:fill-yellow-400" />
                    </div>
                    <div>
                      <p className="text-sm text-muted-foreground mb-1">GitHub Stars</p>
                      <p className="text-3xl font-bold">
                        {githubStars !== undefined ? githubStars.toLocaleString() : 'N/A'}
                      </p>
                    </div>
                  </div>
                  {serverData.repository?.url && (
                    <a
                      href={serverData.repository.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="flex items-center gap-2 text-sm text-blue-600 hover:underline"
                    >
                      View Repository <ExternalLink className="h-4 w-4" />
                    </a>
                  )}
                </div>

                {githubStars === undefined && (
                  <div className="text-center p-8 bg-muted rounded-lg">
                    <Star className="h-12 w-12 mx-auto mb-3 text-muted-foreground opacity-50" />
                    <p className="text-muted-foreground mb-2">No GitHub stars data available</p>
                    <p className="text-sm text-muted-foreground">
                      {serverData.repository 
                        ? "Star data will be fetched on next import/refresh"
                        : "This server doesn't have a GitHub repository configured"}
                    </p>
                  </div>
                )}

                {githubStars !== undefined && (
                  <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                    <Card className="p-4 text-center">
                      <p className="text-sm text-muted-foreground mb-2">Popularity Tier</p>
                      <p className="text-xl font-bold">
                        {githubStars >= 1000 ? '🔥 Very Popular' : 
                         githubStars >= 100 ? '⭐ Popular' : 
                         githubStars >= 10 ? '👍 Growing' : 
                         '🌱 New'}
                      </p>
                    </Card>
                    
                    <Card className="p-4 text-center">
                      <p className="text-sm text-muted-foreground mb-2">Community Size</p>
                      <p className="text-xl font-bold">
                        {githubStars >= 1000 ? 'Large' : 
                         githubStars >= 100 ? 'Medium' : 
                         'Small'}
                      </p>
                    </Card>
                    
                    <Card className="p-4 text-center">
                      <p className="text-sm text-muted-foreground mb-2">Trust Level</p>
                      <p className="text-xl font-bold">
                        {githubStars >= 500 ? 'High' : 
                         githubStars >= 50 ? 'Medium' : 
                         'Building'}
                      </p>
                    </Card>
                  </div>
                )}

                <div className="text-xs text-muted-foreground p-4 bg-muted rounded-lg">
                  <p className="font-semibold mb-2">About Score Metrics:</p>
                  <ul className="space-y-1 ml-4 list-disc">
                    <li>GitHub stars indicate community interest and adoption</li>
                    <li>Metrics are updated during import/refresh operations</li>
                    <li>Higher scores generally indicate more mature and well-maintained servers</li>
                  </ul>
                </div>
              </div>
            </Card>
          </TabsContent>

          <TabsContent value="packages" className="space-y-4">
            {serverData.packages && serverData.packages.length > 0 ? (
              <div className="space-y-3">
                {serverData.packages.map((pkg, i) => (
                  <Card key={i} className="p-4">
                    <div className="flex items-start justify-between mb-3">
                      <div className="flex items-center gap-2">
                        <Package className="h-5 w-5 text-primary" />
                        <h4 className="font-semibold">{pkg.identifier}</h4>
                      </div>
                      <Badge variant="outline">{pkg.registryType}</Badge>
                    </div>
                    <div className="space-y-2 text-sm">
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">Version</span>
                        <span className="font-mono">{pkg.version}</span>
                      </div>
                    </div>
                  </Card>
                ))}
              </div>
            ) : (
              <Card className="p-8">
                <p className="text-center text-muted-foreground">No packages defined</p>
              </Card>
            )}
          </TabsContent>

          <TabsContent value="remotes" className="space-y-4">
            {serverData.remotes && serverData.remotes.length > 0 ? (
              <div className="space-y-3">
                {serverData.remotes.map((remote, i) => (
                  <Card key={i} className="p-4">
                    <div className="flex items-start justify-between mb-3">
                      <div className="flex items-center gap-2">
                        <Server className="h-5 w-5 text-primary" />
                        <h4 className="font-semibold">Remote {i + 1}</h4>
                      </div>
                      <Badge variant="outline">{remote.type}</Badge>
                    </div>
                    {remote.url && (
                      <div className="space-y-2 text-sm">
                        <div className="flex items-center gap-2">
                          <Link className="h-4 w-4 text-muted-foreground" />
                          <a
                            href={remote.url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="text-blue-600 hover:underline break-all"
                          >
                            {remote.url}
                          </a>
                        </div>
                      </div>
                    )}
                  </Card>
                ))}
              </div>
            ) : (
              <Card className="p-8">
                <p className="text-center text-muted-foreground">No remotes defined</p>
              </Card>
            )}
          </TabsContent>

          <TabsContent value="raw">
            <Card className="p-6">
              <div className="flex items-center justify-between mb-4">
                <h3 className="text-lg font-semibold flex items-center gap-2">
                  <Code className="h-5 w-5" />
                  Raw JSON Data
                </h3>
              </div>
              <pre className="bg-muted p-4 rounded-lg overflow-x-auto text-xs">
                {JSON.stringify(server, null, 2)}
              </pre>
            </Card>
          </TabsContent>
        </Tabs>
      </div>
    </div>
  )
}

