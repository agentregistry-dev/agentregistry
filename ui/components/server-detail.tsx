"use client"

import { useState, useEffect } from "react"
import { ServerResponse, adminApiClient } from "@/lib/admin-api"
import { Card } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { toast } from "sonner"
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
  History,
  Check,
  Shield,
  GitFork,
  Eye,
  Zap,
  CheckCircle,
  Clock,
  ShieldCheck,
  BadgeCheck,
} from "lucide-react"

interface ServerDetailProps {
  server: ServerResponse & { allVersions?: ServerResponse[] }
  onClose: () => void
  onServerCopied?: () => void
  onPublish?: (server: ServerResponse) => void
}

export function ServerDetail({ server, onClose, onServerCopied, onPublish }: ServerDetailProps) {
  const [activeTab, setActiveTab] = useState("overview")
  const [copying, setCopying] = useState(false)
  const [copySuccess, setCopySuccess] = useState(false)
  const [copyError, setCopyError] = useState<string | null>(null)
  const [selectedVersion, setSelectedVersion] = useState<ServerResponse>(server)
  const [jsonCopied, setJsonCopied] = useState(false)
  
  // Get all versions, defaulting to just the current server if not available
  const allVersions = server.allVersions || [server]
  
  const { server: serverData, _meta } = selectedVersion
  const official = _meta?.['io.modelcontextprotocol.registry/official']
  
  // Extract metadata
  const publisherMetadata = serverData._meta?.['io.modelcontextprotocol.registry/publisher-provided']?.['aregistry.ai/metadata']
  const githubStars = publisherMetadata?.stars
  const overallScore = publisherMetadata?.score
  const openSSFScore = publisherMetadata?.scorecard?.openssf
  const repoData = publisherMetadata?.repo
  const endpointHealth = publisherMetadata?.endpoint_health
  const scanData = publisherMetadata?.scans
  const identityData = publisherMetadata?.identity
  const semverData = publisherMetadata?.semver
  const securityScanning = publisherMetadata?.security_scanning

  // Get the first icon if available
  const icon = serverData.icons?.[0]

  // Check if there's any score data available
  const hasScoreData = !!(
    overallScore !== undefined ||
    openSSFScore !== undefined ||
    githubStars !== undefined ||
    repoData ||
    endpointHealth ||
    scanData ||
    securityScanning
  )

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

  // Handle version change
  const handleVersionChange = (version: string) => {
    const newVersion = allVersions.find(v => v.server.version === version)
    if (newVersion) {
      setSelectedVersion(newVersion)
    }
  }

  const handlePublishServer = async () => {
    if (onPublish) {
      onPublish(selectedVersion)
      return
    }

    setCopying(true)
    setCopyError(null)
    setCopySuccess(false)

    try {
      // Copy the server data to create a new entry
      await adminApiClient.publishServerStatus(serverData.name, serverData.version)
      toast.success(`Successfully published ${serverData.name}`)
      setCopySuccess(true)
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : "Failed to publish server"
      toast.error(errorMsg)
      setCopyError(errorMsg)
    } finally {
      setCopying(false)
    }
  }

  const handleCopyJson = async () => {
    try {
      await navigator.clipboard.writeText(JSON.stringify(selectedVersion, null, 2))
      setJsonCopied(true)
      setTimeout(() => {
        setJsonCopied(false)
      }, 2000)
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
    <TooltipProvider>
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
                <Tooltip>
                  <TooltipTrigger asChild>
                    <ShieldCheck 
                      className={`h-6 w-6 flex-shrink-0 ${
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
                      className={`h-6 w-6 flex-shrink-0 ${
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
              <p className="text-muted-foreground">{serverData.name}</p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="outline"
                  onClick={handlePublishServer}
                  disabled={copying}
                  className="gap-2"
                >
                  {copying ? (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  ) : (
                    <Copy className="h-4 w-4" />
                  )}
                  Publish
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                <p>Publish this server to your registry</p>
              </TooltipContent>
            </Tooltip>
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
                Server successfully publish to your registry!
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

        {/* Version Selector and Quick Info */}
        {allVersions.length > 1 && (
          <Card className="p-4 mb-6 bg-accent/50 border-primary/20">
            <div className="flex items-center gap-4 flex-wrap">
              <div className="flex items-center gap-2">
                <History className="h-4 w-4 text-muted-foreground" />
                <span className="text-sm font-medium">
                  {allVersions.length} versions available
                </span>
              </div>
              <div className="flex items-center gap-2">
                <span className="text-sm text-muted-foreground">Select version:</span>
                <Select value={selectedVersion.server.version} onValueChange={handleVersionChange}>
                  <SelectTrigger className="w-[180px] h-8">
                    <SelectValue placeholder="Select version" />
                  </SelectTrigger>
                  <SelectContent>
                    {allVersions.map((version) => (
                      <SelectItem key={version.server.version} value={version.server.version}>
                        {version.server.version}
                        {version.server.version === server.server.version && (
                          <span className="ml-2 text-xs text-primary">(Latest)</span>
                        )}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
          </Card>
        )}

        {/* Quick Info */}
        <div className="flex flex-wrap gap-3 mb-6 text-sm">
          <div className="flex items-center gap-2 px-3 py-2 bg-muted rounded-md">
            <Tag className="h-3.5 w-3.5 text-muted-foreground" />
            <span className="text-muted-foreground">Version:</span>
            <span className="font-medium">{serverData.version}</span>
            {allVersions.length > 1 && (
              <Badge variant="secondary" className="ml-2 text-xs">
                {allVersions.length} total
              </Badge>
            )}
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
            {/* Overall Scores */}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              {/* Overall Score */}
              {overallScore !== undefined && (
                <Card className="p-6 bg-gradient-to-br from-blue-50 to-indigo-50 dark:from-blue-950/30 dark:to-indigo-950/30 border-blue-200 dark:border-blue-800">
                  <div className="flex items-center gap-4">
                    <div className="p-3 bg-blue-100 dark:bg-blue-900/30 rounded-full">
                      <TrendingUp className="h-8 w-8 text-blue-600 dark:text-blue-400" />
                    </div>
                    <div>
                      <p className="text-sm text-muted-foreground mb-1">Overall Score</p>
                      <p className="text-4xl font-bold">{overallScore.toFixed(2)}</p>
                    </div>
                  </div>
                </Card>
              )}

              {/* OpenSSF Scorecard */}
              {openSSFScore !== undefined && (
                <Card className="p-6 bg-gradient-to-br from-green-50 to-emerald-50 dark:from-green-950/30 dark:to-emerald-950/30 border-green-200 dark:border-green-800">
                  <div className="flex items-center gap-4">
                    <div className="p-3 bg-green-100 dark:bg-green-900/30 rounded-full">
                      <Shield className="h-8 w-8 text-green-600 dark:text-green-400" />
                    </div>
                    <div>
                      <p className="text-sm text-muted-foreground mb-1">OpenSSF Scorecard</p>
                      <p className="text-4xl font-bold">{openSSFScore.toFixed(1)}/10</p>
                    </div>
                  </div>
                </Card>
              )}
            </div>

            {/* GitHub Repository Stats */}
            {(githubStars !== undefined || repoData) && (
              <Card className="p-6">
                <h3 className="text-lg font-semibold mb-4 flex items-center gap-2">
                  <GitBranch className="h-5 w-5" />
                  Repository Statistics
                </h3>
                <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
                  {githubStars !== undefined && (
                    <div className="flex items-center justify-between p-4 bg-gradient-to-r from-yellow-50 to-orange-50 dark:from-yellow-950/20 dark:to-orange-950/20 rounded-lg border border-yellow-200 dark:border-yellow-800">
                      <div className="flex items-center gap-3">
                        <Star className="h-6 w-6 text-yellow-600 dark:text-yellow-400 fill-yellow-600 dark:fill-yellow-400" />
                        <div>
                          <p className="text-xs text-muted-foreground">Stars</p>
                          <p className="text-2xl font-bold">{githubStars.toLocaleString()}</p>
                        </div>
                      </div>
                    </div>
                  )}

                  {repoData?.forks_count !== undefined && (
                    <div className="flex items-center justify-between p-4 bg-muted rounded-lg">
                      <div className="flex items-center gap-3">
                        <GitFork className="h-6 w-6 text-primary" />
                        <div>
                          <p className="text-xs text-muted-foreground">Forks</p>
                          <p className="text-2xl font-bold">{repoData.forks_count.toLocaleString()}</p>
                        </div>
                      </div>
                    </div>
                  )}

                  {repoData?.watchers_count !== undefined && (
                    <div className="flex items-center justify-between p-4 bg-muted rounded-lg">
                      <div className="flex items-center gap-3">
                        <Eye className="h-6 w-6 text-primary" />
                        <div>
                          <p className="text-xs text-muted-foreground">Watchers</p>
                          <p className="text-2xl font-bold">{repoData.watchers_count.toLocaleString()}</p>
                        </div>
                      </div>
                    </div>
                  )}

                  {repoData?.primary_language && (
                    <div className="flex items-center justify-between p-4 bg-muted rounded-lg">
                      <div className="flex items-center gap-3">
                        <Code className="h-6 w-6 text-primary" />
                        <div>
                          <p className="text-xs text-muted-foreground">Language</p>
                          <p className="text-lg font-bold">{repoData.primary_language}</p>
                        </div>
                      </div>
                    </div>
                  )}
                </div>
                {serverData.repository?.url && (
                  <div className="mt-4 pt-4 border-t">
                    <a
                      href={serverData.repository.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="flex items-center gap-2 text-sm text-blue-600 hover:underline"
                    >
                      <ExternalLink className="h-4 w-4" />
                      View Repository
                    </a>
                  </div>
                )}
              </Card>
            )}

            {/* Endpoint Health */}
            {endpointHealth && (
              <Card className="p-6">
                <h3 className="text-lg font-semibold mb-4 flex items-center gap-2">
                  <Zap className="h-5 w-5" />
                  Endpoint Health
                </h3>
                <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                  <div className="flex items-center gap-3 p-4 bg-muted rounded-lg">
                    <div className={`p-2 rounded-full ${endpointHealth.reachable ? 'bg-green-100 dark:bg-green-900/30' : 'bg-red-100 dark:bg-red-900/30'}`}>
                      <CheckCircle className={`h-5 w-5 ${endpointHealth.reachable ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'}`} />
                    </div>
                    <div>
                      <p className="text-xs text-muted-foreground">Status</p>
                      <p className="text-lg font-bold">
                        {endpointHealth.reachable ? 'Reachable' : 'Unreachable'}
                      </p>
                    </div>
                  </div>

                  {endpointHealth.response_ms !== undefined && (
                    <div className="flex items-center gap-3 p-4 bg-muted rounded-lg">
                      <div className="p-2 bg-blue-100 dark:bg-blue-900/30 rounded-full">
                        <Clock className="h-5 w-5 text-blue-600 dark:text-blue-400" />
                      </div>
                      <div>
                        <p className="text-xs text-muted-foreground">Response Time</p>
                        <p className="text-lg font-bold">{endpointHealth.response_ms}ms</p>
                      </div>
                    </div>
                  )}

                  {endpointHealth.last_checked_at && (
                    <div className="flex items-center gap-3 p-4 bg-muted rounded-lg">
                      <div className="p-2 bg-purple-100 dark:bg-purple-900/30 rounded-full">
                        <Calendar className="h-5 w-5 text-purple-600 dark:text-purple-400" />
                      </div>
                      <div>
                        <p className="text-xs text-muted-foreground">Last Checked</p>
                        <p className="text-sm font-medium">{new Date(endpointHealth.last_checked_at).toLocaleDateString()}</p>
                      </div>
                    </div>
                  )}
                </div>
              </Card>
            )}

            {/* Security & Scanning */}
            {(scanData || securityScanning) && (
              <Card className="p-6">
                <h3 className="text-lg font-semibold mb-4 flex items-center gap-2">
                  <Shield className="h-5 w-5" />
                  Security & Scanning
                </h3>
                
                {/* Dependency Health */}
                {scanData?.dependency_health && (
                  <div className="mb-6">
                    <h4 className="text-sm font-semibold mb-3 text-muted-foreground">Dependency Health</h4>
                    <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
                      <div className="p-3 bg-muted rounded-lg text-center">
                        <p className="text-xs text-muted-foreground mb-1">Total Packages</p>
                        <p className="text-xl font-bold">{scanData.dependency_health.packages_total}</p>
                      </div>
                      <div className="p-3 bg-muted rounded-lg text-center">
                        <p className="text-xs text-muted-foreground mb-1">Copyleft Licenses</p>
                        <p className="text-xl font-bold">{scanData.dependency_health.copyleft_licenses}</p>
                      </div>
                      <div className="p-3 bg-muted rounded-lg text-center">
                        <p className="text-xs text-muted-foreground mb-1">Unknown Licenses</p>
                        <p className="text-xl font-bold">{scanData.dependency_health.unknown_licenses}</p>
                      </div>
                      {scanData.dependency_health.ecosystems && (
                        <div className="p-3 bg-muted rounded-lg text-center">
                          <p className="text-xs text-muted-foreground mb-1">Ecosystems</p>
                          <div className="text-sm font-bold space-y-1">
                            {Object.entries(scanData.dependency_health.ecosystems).map(([key, value]) => (
                              <div key={key}>{key}: {value}</div>
                            ))}
                          </div>
                        </div>
                      )}
                    </div>
                  </div>
                )}

                {/* Security Scanning */}
                {securityScanning && (
                  <div className="mb-6">
                    <h4 className="text-sm font-semibold mb-3 text-muted-foreground">Security Tools</h4>
                    <div className="grid grid-cols-2 md:grid-cols-3 gap-3">
                      <div className="flex items-center gap-2 p-3 bg-muted rounded-lg">
                        <CheckCircle className={`h-4 w-4 ${securityScanning.codeql_enabled ? 'text-green-600' : 'text-gray-400'}`} />
                        <span className="text-sm">CodeQL</span>
                      </div>
                      <div className="flex items-center gap-2 p-3 bg-muted rounded-lg">
                        <CheckCircle className={`h-4 w-4 ${securityScanning.dependabot_enabled ? 'text-green-600' : 'text-gray-400'}`} />
                        <span className="text-sm">Dependabot</span>
                      </div>
                    </div>
                  </div>
                )}

                {/* Scan Details */}
                {scanData?.details && scanData.details.length > 0 && (
                  <div>
                    <h4 className="text-sm font-semibold mb-3 text-muted-foreground">Scan Details</h4>
                    <div className="space-y-2">
                      {scanData.details.map((detail: string, idx: number) => (
                        <div key={idx} className="text-xs p-2 bg-muted rounded font-mono">
                          {detail}
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                {/* Scan Summary */}
                {scanData?.summary && (
                  <div className="mt-4 p-3 bg-blue-50 dark:bg-blue-950/30 rounded-lg border border-blue-200 dark:border-blue-800">
                    <p className="text-xs font-semibold text-muted-foreground mb-1">Summary</p>
                    <p className="text-sm font-mono">{scanData.summary}</p>
                  </div>
                )}
              </Card>
            )}

            {/* Info Box */}
            {!publisherMetadata && (
              <div className="text-center p-8 bg-muted rounded-lg">
                <TrendingUp className="h-12 w-12 mx-auto mb-3 text-muted-foreground opacity-50" />
                <p className="text-muted-foreground mb-2">No scoring data available</p>
                <p className="text-sm text-muted-foreground">
                  Scoring data will be fetched on next import/refresh from the registry
                </p>
              </div>
            )}
          </TabsContent>

          <TabsContent value="packages" className="space-y-4">
            {serverData.packages && serverData.packages.length > 0 ? (
              <div className="space-y-4">
                {serverData.packages.map((pkg, i) => (
                  <Card key={i} className="p-6">
                    <div className="flex items-start justify-between mb-4">
                      <div className="flex items-center gap-2">
                        <Package className="h-5 w-5 text-primary" />
                        <h4 className="font-semibold text-lg">{pkg.identifier}</h4>
                      </div>
                      <Badge variant="outline">{pkg.registryType}</Badge>
                    </div>
                    
                    {/* Basic package info */}
                    <div className="space-y-2 text-sm mb-4 pb-4 border-b">
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">Version</span>
                        <span className="font-mono">{pkg.version}</span>
                      </div>
                      {(pkg as any).runtimeHint && (
                        <div className="flex justify-between">
                          <span className="text-muted-foreground">Runtime</span>
                          <Badge variant="secondary">{(pkg as any).runtimeHint}</Badge>
                        </div>
                      )}
                      {(pkg as any).transport?.type && (
                        <div className="flex justify-between">
                          <span className="text-muted-foreground">Transport</span>
                          <Badge variant="secondary">{(pkg as any).transport.type}</Badge>
                        </div>
                      )}
                    </div>

                    {/* Runtime Arguments */}
                    <RuntimeArgumentsTable arguments={(pkg as any).runtimeArguments} />

                    {/* Environment Variables */}
                    <EnvironmentVariablesTable variables={(pkg as any).environmentVariables} />
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
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleCopyJson}
                  className="gap-2"
                >
                  {jsonCopied ? (
                    <>
                      <Check className="h-4 w-4" />
                      Copied!
                    </>
                  ) : (
                    <>
                      <Copy className="h-4 w-4" />
                      Copy JSON
                    </>
                  )}
                </Button>
              </div>
              <pre className="bg-muted p-4 rounded-lg overflow-x-auto text-xs">
                {JSON.stringify(selectedVersion, null, 2)}
              </pre>
            </Card>
          </TabsContent>
        </Tabs>
        </div>
      </div>
    </TooltipProvider>
  )
}

