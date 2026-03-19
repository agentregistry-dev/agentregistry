"use client"

import { useEffect, useState } from "react"
import Link from "next/link"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { listDeployments, removeDeployment, Deployment } from "@/lib/admin-api"
import { Input } from "@/components/ui/input"
import { Trash2, AlertCircle, Calendar, Package, Copy, Check, Link2, Server, Bot as BotIcon, Search, X } from "lucide-react"
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
import { toast } from "sonner"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

const GATEWAY_BASE_URL = process.env.NEXT_PUBLIC_GATEWAY_URL || "http://localhost:21212"

const STATUS_COLORS: Record<string, string> = {
  deployed:   'bg-green-500',
  discovered: 'bg-green-500',
  deploying:  'bg-amber-500',
  failed:     'bg-destructive',
  cancelled:  'bg-muted-foreground',
}

function sanitizeName(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9-]/g, '-')
}

function getAgentEndpointUrl(serverName: string, deploymentId: string): string {
  const name = sanitizeName(serverName)
  const id = sanitizeName(deploymentId)
  return `${GATEWAY_BASE_URL}/agents/${name}-${id}`
}

export default function DeployedPage() {
  const [deployments, setDeployments] = useState<Deployment[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [removing, setRemoving] = useState(false)
  const [serverToRemove, setServerToRemove] = useState<{ id: string, name: string, version: string, resourceType: string } | null>(null)
  const [copied, setCopied] = useState(false)
  const [copiedAgentId, setCopiedAgentId] = useState<string | null>(null)
  const [searchQuery, setSearchQuery] = useState("")
  const [filterProvider, setFilterProvider] = useState<string>("all")
  const [filterOrigin, setFilterOrigin] = useState<string>("all")
  const [filterStatus, setFilterStatus] = useState<string>("all")

  const gatewayUrl = `${GATEWAY_BASE_URL}/mcp`

  const copyToClipboard = () => {
    navigator.clipboard.writeText(gatewayUrl)
    setCopied(true)
    toast.success("Gateway URL copied to clipboard!")
    setTimeout(() => setCopied(false), 2000)
  }

  const copyAgentUrl = (deploymentId: string, url: string) => {
    navigator.clipboard.writeText(url)
    setCopiedAgentId(deploymentId)
    toast.success("Agent endpoint URL copied to clipboard!")
    setTimeout(() => setCopiedAgentId(null), 2000)
  }

  const fetchDeployments = async () => {
    try {
      setError(null)
      const { data: deployData } = await listDeployments({ throwOnError: true })
      setDeployments(deployData.deployments)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch deployments')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchDeployments()
    const interval = setInterval(fetchDeployments, 30000)
    return () => clearInterval(interval)
  }, [])

  const handleRemove = async (deployment: Deployment) => {
    setServerToRemove({ id: deployment.id, name: deployment.serverName, version: deployment.version, resourceType: deployment.resourceType })
  }

  const confirmRemove = async () => {
    if (!serverToRemove) return

    try {
      setRemoving(true)
      await removeDeployment({ path: { id: serverToRemove.id }, throwOnError: true })
      setDeployments(prev => prev.filter(d => d.id !== serverToRemove.id))
      setServerToRemove(null)
      fetchDeployments()
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to remove deployment')
    } finally {
      setRemoving(false)
    }
  }

  const uniqueProviders = [...new Set(deployments.map(d => d.providerId || "local"))]
  const uniqueOrigins = [...new Set(deployments.map(d => d.origin))]
  const uniqueStatuses = [...new Set(deployments.map(d => d.status))]

  const filtered = deployments.filter(d => {
    if (filterProvider !== "all" && (d.providerId || "local") !== filterProvider) return false
    if (filterOrigin !== "all" && d.origin !== filterOrigin) return false
    if (filterStatus !== "all" && d.status !== filterStatus) return false
    if (searchQuery) {
      const q = searchQuery.toLowerCase()
      if (!d.serverName.toLowerCase().includes(q) && !d.version.toLowerCase().includes(q)) return false
    }
    return true
  })

  const hasActiveFilters = filterProvider !== "all" || filterOrigin !== "all" || filterStatus !== "all"

  const agents = filtered.filter(d => d.resourceType === 'agent')
  const mcpServers = filtered.filter(d => d.resourceType === 'mcp')
  const runningCount = deployments.filter(d => d.status === 'deployed' || d.status === 'discovered').length

  return (
    <main className="bg-background">
      <div className="container mx-auto px-6">
        {/* Header */}
        <div className="flex items-center justify-between border-b py-4">
          <div>
            <h1 className="text-xl font-semibold">Deployed Resources</h1>
            <p className="text-[15px] text-muted-foreground">
              {runningCount} resource{runningCount !== 1 ? 's' : ''} running
            </p>
          </div>
        </div>

        {error && (
          <div className="flex items-center gap-2 text-destructive py-3 text-sm">
            <AlertCircle className="h-4 w-4" />
            <p>{error}</p>
          </div>
        )}

        {/* Search and filters */}
        {!loading && deployments.length > 0 && (
          <div className="flex items-center gap-3 py-4">
            <div className="relative flex-1 max-w-sm">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
              <Input
                placeholder="Search deployments..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="pl-9 h-10 text-[15px]"
              />
            </div>

            <div className="flex items-center gap-2 ml-auto">
              <Select value={filterProvider} onValueChange={setFilterProvider}>
                <SelectTrigger className="w-[140px] h-8 text-sm">
                  <SelectValue placeholder="Provider" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All providers</SelectItem>
                  {uniqueProviders.map(p => (
                    <SelectItem key={p} value={p}>{p}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select value={filterOrigin} onValueChange={setFilterOrigin}>
                <SelectTrigger className="w-[140px] h-8 text-sm">
                  <SelectValue placeholder="Origin" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All origins</SelectItem>
                  {uniqueOrigins.map(o => (
                    <SelectItem key={o} value={o}>{o}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select value={filterStatus} onValueChange={setFilterStatus}>
                <SelectTrigger className="w-[140px] h-8 text-sm">
                  <SelectValue placeholder="Status" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All statuses</SelectItem>
                  {uniqueStatuses.map(s => (
                    <SelectItem key={s} value={s}>{s}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {hasActiveFilters && (
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-8 gap-1 text-xs text-muted-foreground"
                  onClick={() => { setFilterProvider("all"); setFilterOrigin("all"); setFilterStatus("all") }}
                >
                  <X className="h-3 w-3" />
                  Clear
                </Button>
              )}
            </div>
          </div>
        )}

        <div className="py-6">
          {loading ? (
            <div className="flex items-center justify-center py-20">
              <div className="text-center space-y-3">
                <div className="h-8 w-8 animate-spin rounded-full border-2 border-primary border-t-transparent mx-auto" />
                <p className="text-sm text-muted-foreground">Loading resources...</p>
              </div>
            </div>
          ) : (agents.length === 0 && mcpServers.length === 0) ? (
            <div className="flex flex-col items-center justify-center py-20 text-center">
              <Server className="h-8 w-8 text-muted-foreground mb-4 opacity-40" />
              <p className="text-base font-medium mb-1">No resources deployed</p>
              <p className="text-sm text-muted-foreground mb-4 max-w-xs">
                Deploy MCP servers or agents from the Catalog to see them here.
              </p>
              <Link href="/">
                <Button size="sm" variant="outline">Go to Catalog</Button>
              </Link>
            </div>
          ) : (
            <div className="space-y-8">
              {/* Agents */}
              {agents.length > 0 && (
                <section>
                  <div className="flex items-center gap-2 mb-3">
                    <h2 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground">Agents</h2>
                    <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4">
                      {agents.length}
                    </Badge>
                  </div>
                  <div className="divide-y">
                    {agents.map((item) => (
                      <DeploymentRow
                        key={item.id}
                        item={item}
                        onRemove={handleRemove}
                        removing={removing}
                        copiedAgentId={copiedAgentId}
                        onCopyAgentUrl={copyAgentUrl}
                        getAgentEndpointUrl={getAgentEndpointUrl}
                      />
                    ))}
                  </div>
                </section>
              )}

              {/* MCP Servers */}
              {mcpServers.length > 0 && (
                <section>
                  <div className="flex items-center gap-2 mb-3">
                    <h2 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground">MCP Servers</h2>
                    <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4">
                      {mcpServers.length}
                    </Badge>
                  </div>
                  <div className="flex items-center gap-3 px-4 py-3 bg-muted/60 border rounded-lg mb-4">
                    <Link2 className="h-4 w-4 text-primary shrink-0" />
                    <div className="flex-1 min-w-0">
                      <p className="text-xs text-muted-foreground mb-0.5">Gateway URL — connect MCP clients to all managed servers (local provider only)</p>
                      <code className="text-sm font-mono text-foreground">{gatewayUrl}</code>
                    </div>
                    <button
                      onClick={copyToClipboard}
                      className="text-muted-foreground hover:text-foreground transition-colors shrink-0"
                      title="Copy gateway URL"
                    >
                      {copied ? (
                        <Check className="h-4 w-4 text-green-500" />
                      ) : (
                        <Copy className="h-4 w-4" />
                      )}
                    </button>
                  </div>
                  <div className="divide-y">
                    {mcpServers.map((item) => (
                      <DeploymentRow
                        key={item.id}
                        item={item}
                        onRemove={handleRemove}
                        removing={removing}
                        copiedAgentId={copiedAgentId}
                        onCopyAgentUrl={copyAgentUrl}
                        getAgentEndpointUrl={getAgentEndpointUrl}
                      />
                    ))}
                  </div>
                </section>
              )}
            </div>
          )}
        </div>
      </div>

      {/* Remove Confirmation Dialog */}
      <Dialog open={!!serverToRemove} onOpenChange={(open) => !open && setServerToRemove(null)}>
        <DialogContent onClose={() => setServerToRemove(null)}>
          <DialogHeader>
            <DialogTitle>Remove Deployment</DialogTitle>
            <DialogDescription>
              Are you sure you want to remove <strong>{serverToRemove?.name}</strong> (version {serverToRemove?.version}) ({serverToRemove?.resourceType})?
              <br /><br />
              This will stop the server and remove it from your deployments. This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setServerToRemove(null)} disabled={removing}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={confirmRemove} disabled={removing}>
              {removing ? 'Removing...' : 'Remove'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </main>
  )
}

function DeploymentRow({ item, onRemove, removing, copiedAgentId, onCopyAgentUrl, getAgentEndpointUrl }: {
  item: Deployment
  onRemove: (d: Deployment) => void
  removing: boolean
  copiedAgentId: string | null
  onCopyAgentUrl: (id: string, url: string) => void
  getAgentEndpointUrl: (name: string, id: string) => string
}) {
  const isAgent = item.resourceType === 'agent'
  const [showError, setShowError] = useState(false)
  const hasError = Boolean(item.error)

  const statusColor = STATUS_COLORS[item.status] ?? 'bg-muted-foreground'

  return (
    <TooltipProvider>
      <div className="group flex items-start gap-3.5 py-4 px-2 -mx-2 rounded-md transition-colors hover:bg-muted/50">
        <div className="w-10 h-10 rounded bg-primary/8 flex items-center justify-center flex-shrink-0 mt-0.5">
          {isAgent ? <BotIcon className="h-4 w-4 text-primary" /> : <Server className="h-4 w-4 text-primary" />}
        </div>

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-0.5">
            <Tooltip>
              <TooltipTrigger asChild>
                <span className={`inline-block h-2 w-2 rounded-full shrink-0 ${statusColor} ${hasError && item.error ? 'cursor-pointer' : ''}`}
                  onClick={() => hasError && item.error && setShowError(!showError)}
                />
              </TooltipTrigger>
              <TooltipContent side="right">
                <p>{item.status}{hasError ? ' — click to show error' : ''}</p>
              </TooltipContent>
            </Tooltip>
            <h3 className="text-lg font-semibold">{item.serverName}</h3>
          </div>

          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted-foreground mt-1">
            <span className="font-mono">{item.version}</span>
            <span>{item.providerId || "local"}</span>
            <span>{item.origin}</span>
            <span className="flex items-center gap-1">
              <Calendar className="h-3 w-3" />
              {new Date(item.deployedAt).toLocaleDateString()}
            </span>
            {item.env?.namespace && (
              <span className="font-mono">{item.env.namespace}</span>
            )}
          </div>

          {isAgent && (!item.providerId || item.providerId === 'local') && (
            <div className="flex items-center gap-2 mt-2.5 px-3 py-2 bg-muted/60 border rounded-md">
              <Link2 className="h-3.5 w-3.5 text-primary shrink-0" />
              <code className="text-sm font-mono text-foreground truncate flex-1">
                {getAgentEndpointUrl(item.serverName, item.id)}
              </code>
              <button
                className="text-muted-foreground hover:text-foreground transition-colors shrink-0 ml-1"
                onClick={() => onCopyAgentUrl(item.id, getAgentEndpointUrl(item.serverName, item.id))}
                title="Copy endpoint URL"
              >
                {copiedAgentId === item.id ? (
                  <Check className="h-4 w-4 text-green-500" />
                ) : (
                  <Copy className="h-4 w-4" />
                )}
              </button>
            </div>
          )}

          {showError && item.error && (
            <div className="flex items-start gap-2 mt-2.5 px-3 py-2 bg-destructive/5 border border-destructive/20 rounded-md">
              <AlertCircle className="h-3.5 w-3.5 text-destructive shrink-0 mt-0.5" />
              <p className="text-sm text-destructive break-all">{item.error}</p>
            </div>
          )}
        </div>

        {item.origin === 'managed' && (
          <Button
            variant="ghost"
            size="sm"
            className="h-7 gap-1 text-xs text-destructive hover:text-destructive hover:bg-destructive/10 opacity-0 group-hover:opacity-100 transition-opacity shrink-0"
            onClick={() => onRemove(item)}
            disabled={removing}
          >
            <Trash2 className="h-3 w-3" />
            Remove
          </Button>
        )}
      </div>
    </TooltipProvider>
  )
}
