"use client"

import { useEffect, useState } from "react"
import Link from "next/link"
import { Card } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { adminApiClient } from "@/lib/admin-api"
import { Trash2, AlertCircle, Calendar, Package, Copy, Check } from "lucide-react"
import { toast } from "sonner"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

type DeploymentResponse = {
  serverName: string
  version: string
  deployedAt: string
  updatedAt: string
  status: string
  config: Record<string, string>
  preferRemote: boolean
  resourceType: string // "mcp" or "agent"
}

export default function DeployedPage() {
  const [deployments, setDeployments] = useState<DeploymentResponse[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [removing, setRemoving] = useState(false)
  const [serverToRemove, setServerToRemove] = useState<{ name: string, version: string } | null>(null)
  const [copied, setCopied] = useState(false)

  const gatewayUrl = "http://localhost:21212/mcp"

  const copyToClipboard = () => {
    navigator.clipboard.writeText(gatewayUrl)
    setCopied(true)
    toast.success("Gateway URL copied to clipboard!")
    setTimeout(() => setCopied(false), 2000)
  }

  const fetchDeployments = async () => {
    try {
      setLoading(true)
      setError(null)
      const data = await adminApiClient.listDeployments()
      setDeployments(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch deployments')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchDeployments()
    // Refresh every 30 seconds
    const interval = setInterval(fetchDeployments, 30000)
    return () => clearInterval(interval)
  }, [])

  const handleRemove = async (serverName: string, version: string) => {
    setServerToRemove({ name: serverName, version })
  }

  const confirmRemove = async () => {
    if (!serverToRemove) return

    try {
      setRemoving(true)
      await adminApiClient.removeDeployment(serverToRemove.name, serverToRemove.version)
      
      // Remove from local state
      setDeployments(prev => prev.filter(d => d.serverName !== serverToRemove.name || d.version !== serverToRemove.version))
      setServerToRemove(null)
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to remove deployment')
    } finally {
      setRemoving(false)
    }
  }

  const runningCount = deployments.length
  
  return (
    <main className="min-h-screen bg-background">
      {/* Stats Section */}
      <div className="bg-muted/30 border-b">
        <div className="container mx-auto px-6 py-6">
          <div className="grid gap-4 md:grid-cols-3">
            <Card className="p-4 hover:shadow-md transition-all duration-200 border hover:border-primary/20">
              <div className="flex items-center gap-3">
                <div className="p-2 bg-green-500/10 rounded-lg flex items-center justify-center">
                  <svg
                    xmlns="http://www.w3.org/2000/svg"
                    fill="none"
                    viewBox="0 0 24 24"
                    strokeWidth={2}
                    stroke="currentColor"
                    className="h-5 w-5 text-green-600"
                  >
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      d="M9 12.75L11.25 15 15 9.75M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                    />
                  </svg>
                </div>
                <div>
                  <p className="text-2xl font-bold">{runningCount}</p>
                  <p className="text-xs text-muted-foreground">Deployed</p>
                </div>
              </div>
            </Card>

            <Card className="p-4 hover:shadow-md transition-all duration-200 border hover:border-primary/20">
              <div className="flex items-center gap-3">
                <div className="p-2 bg-blue-500/10 rounded-lg flex items-center justify-center">
                  <svg
                    xmlns="http://www.w3.org/2000/svg"
                    fill="none"
                    viewBox="0 0 24 24"
                    strokeWidth={2}
                    stroke="currentColor"
                    className="h-5 w-5 text-blue-600"
                  >
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      d="M5.25 14.25h13.5m-13.5 0a3 3 0 01-3-3m3 3a3 3 0 100 6h13.5a3 3 0 100-6m-16.5-3a3 3 0 013-3h13.5a3 3 0 013 3m-19.5 0a4.5 4.5 0 01.9-2.7L5.737 5.1a3.375 3.375 0 012.7-1.35h7.126c1.062 0 2.062.5 2.7 1.35l2.587 3.45a4.5 4.5 0 01.9 2.7m0 0a3 3 0 01-3 3m0 3h.008v.008h-.008v-.008zm0-6h.008v.008h-.008v-.008zm-3 6h.008v.008h-.008v-.008zm0-6h.008v.008h-.008v-.008z"
                    />
                  </svg>
                </div>
                <div>
                  <p className="text-2xl font-bold">{deployments.filter(d => d.resourceType === "mcp").length}</p>
                  <p className="text-xs text-muted-foreground">MCP Servers</p>
                </div>
              </div>
            </Card>

            <Card className="p-4 hover:shadow-md transition-all duration-200 border hover:border-primary/20">
              <div className="flex items-center justify-between gap-3">
                <div className="flex items-center gap-3">
                  <div className="p-2 bg-purple-500/10 rounded-lg flex items-center justify-center">
                    <svg
                      xmlns="http://www.w3.org/2000/svg"
                      fill="none"
                      viewBox="0 0 24 24"
                      strokeWidth={2}
                      stroke="currentColor"
                      className="h-5 w-5 text-purple-600"
                    >
                      <path
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        d="M3.75 13.5l10.5-11.25L12 10.5h8.25L9.75 21.75 12 13.5H3.75z"
                      />
                    </svg>
                  </div>
                  <div>
                    <code className="text-sm font-mono font-semibold">{gatewayUrl}</code>
                    <p className="text-xs text-muted-foreground">Gateway Endpoint</p>
                  </div>
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={copyToClipboard}
                  className="shrink-0"
                  title="Copy gateway URL"
                >
                  {copied ? (
                    <Check className="h-4 w-4 text-green-600" />
                  ) : (
                    <Copy className="h-4 w-4" />
                  )}
                </Button>
              </div>
            </Card>
          </div>
        </div>
      </div>

      <div className="container mx-auto px-6 py-12">
        <div className="max-w-4xl mx-auto">
          <div className="mb-8">
            <h1 className="text-3xl font-bold mb-2">Deployed</h1>
            <p className="text-muted-foreground">
              Monitor and manage MCP servers and agents that are currently deployed on your system.
            </p>
          </div>

          {error && (
            <Card className="p-4 mb-6 bg-destructive/10 border-destructive/20">
              <div className="flex items-center gap-2 text-destructive">
                <AlertCircle className="h-4 w-4" />
                <p className="text-sm font-medium">{error}</p>
              </div>
            </Card>
          )}

          {loading ? (
            <Card className="p-12">
              <div className="text-center text-muted-foreground">
                <p className="text-lg font-medium">Loading deployments...</p>
              </div>
            </Card>
          ) : deployments.length === 0 ? (
            <Card className="p-12">
              <div className="text-center text-muted-foreground">
                <div className="w-16 h-16 mx-auto mb-4 opacity-50 flex items-center justify-center">
                  <svg
                    xmlns="http://www.w3.org/2000/svg"
                    fill="none"
                    viewBox="0 0 24 24"
                    strokeWidth={1.5}
                    stroke="currentColor"
                    className="w-16 h-16"
                  >
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      d="M5.25 14.25h13.5m-13.5 0a3 3 0 01-3-3m3 3a3 3 0 100 6h13.5a3 3 0 100-6m-16.5-3a3 3 0 013-3h13.5a3 3 0 013 3m-19.5 0a4.5 4.5 0 01.9-2.7L5.737 5.1a3.375 3.375 0 012.7-1.35h7.126c1.062 0 2.062.5 2.7 1.35l2.587 3.45a4.5 4.5 0 01.9 2.7m0 0a3 3 0 01-3 3m0 3h.008v.008h-.008v-.008zm0-6h.008v.008h-.008v-.008zm-3 6h.008v.008h-.008v-.008zm0-6h.008v.008h-.008v-.008z"
                    />
                  </svg>
                </div>
                <p className="text-lg font-medium mb-2">
                  No deployed servers
                </p>
                <p className="text-sm mb-6">
                  Deploy MCP servers from the Admin panel to monitor them here.
                </p>
                <Link
                  href="/"
                  className="inline-flex items-center justify-center rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:opacity-50 disabled:pointer-events-none ring-offset-background bg-primary text-primary-foreground hover:bg-primary/90 h-10 py-2 px-4"
                >
                  Go to Admin Panel
                </Link>
              </div>
            </Card>
          ) : (
            <div className="space-y-4">
              {deployments.map((deployment) => (
                <Card key={`${deployment.serverName}-${deployment.version}`} className="p-6 hover:shadow-md transition-all duration-200">
                  <div className="flex items-start justify-between">
                    <div className="flex-1">
                      <div className="flex items-center gap-3 mb-3">
                        <h3 className="text-xl font-semibold">{deployment.serverName}</h3>
                      </div>
                      
                      <div className="grid grid-cols-2 gap-4 text-sm">
                        <div className="flex items-center gap-2 text-muted-foreground">
                          <Calendar className="h-4 w-4" />
                          <span>Deployed: {new Date(deployment.deployedAt).toLocaleString()}</span>
                        </div>
                        <div className="flex items-center gap-2 text-muted-foreground">
                          <Package className="h-4 w-4" />
                          <span>Version: {deployment.version}</span>
                        </div>
                      </div>

                      {Object.keys(deployment.config).length > 0 && (
                        <div className="mt-3 pt-3 border-t">
                          <p className="text-xs text-muted-foreground mb-2">Configuration:</p>
                          <div className="flex flex-wrap gap-2">
                            {Object.entries(deployment.config).slice(0, 3).map(([key, value]) => (
                              <span key={key} className="text-xs px-2 py-1 bg-muted rounded">
                                {key}
                              </span>
                            ))}
                            {Object.keys(deployment.config).length > 3 && (
                              <span className="text-xs px-2 py-1 bg-muted rounded text-muted-foreground">
                                +{Object.keys(deployment.config).length - 3} more
                              </span>
                            )}
                          </div>
                        </div>
                      )}
                    </div>

                    <Button
                      variant="destructive"
                      size="sm"
                      className="ml-4"
                      onClick={() => handleRemove(deployment.serverName, deployment.version)}
                      disabled={removing}
                    >
                      <Trash2 className="h-4 w-4 mr-2" />
                      Remove
                    </Button>
                  </div>
                </Card>
              ))}
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
              Are you sure you want to remove <strong>{serverToRemove?.name}</strong> (version {serverToRemove?.version})?
              <br />
              <br />
              This will stop the server and remove it from your deployments. This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setServerToRemove(null)}
              disabled={removing}
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={confirmRemove}
              disabled={removing}
            >
              {removing ? 'Removing...' : 'Remove Deployment'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </main>
  )
}
