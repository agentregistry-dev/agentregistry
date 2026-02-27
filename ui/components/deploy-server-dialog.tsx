"use client"

import { useState } from "react"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Separator } from "@/components/ui/separator"
import { deployServer as deployServerApi, type ServerResponse } from "@/lib/admin-api"
import { Play, Plus, X, Loader2 } from "lucide-react"

interface DeployServerDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  server: ServerResponse | null
  onDeploySuccess?: () => void
}

export function DeployServerDialog({ open, onOpenChange, server, onDeploySuccess }: DeployServerDialogProps) {
  const [deploying, setDeploying] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState(false)
  const [config, setConfig] = useState<Record<string, string>>({})
  const [newKey, setNewKey] = useState("")
  const [newValue, setNewValue] = useState("")

  const handleAddConfig = () => {
    if (newKey.trim() && newValue.trim()) {
      setConfig({ ...config, [newKey.trim()]: newValue.trim() })
      setNewKey("")
      setNewValue("")
    }
  }

  const handleRemoveConfig = (key: string) => {
    const newConfig = { ...config }
    delete newConfig[key]
    setConfig(newConfig)
  }

  const handleDeploy = async () => {
    if (!server) return

    try {
      setDeploying(true)
      setError(null)
      
      await deployServerApi({
        body: {
          serverName: server.server.name,
          version: server.server.version,
          env: config,
          preferRemote: false,
        },
        throwOnError: true,
      })

      setSuccess(true)
      setTimeout(() => {
        onOpenChange(false)
        setSuccess(false)
        setConfig({})
        onDeploySuccess?.()
      }, 1500)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to deploy server")
    } finally {
      setDeploying(false)
    }
  }

  const handleClose = () => {
    if (!deploying) {
      onOpenChange(false)
      setError(null)
      setSuccess(false)
      setConfig({})
      setNewKey("")
      setNewValue("")
    }
  }

  if (!server) return null

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-w-2xl max-h-[80vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Deploy Server</DialogTitle>
          <DialogDescription>
            Deploy {server.server.title || server.server.name} (v{server.server.version}) to your runtime
          </DialogDescription>
        </DialogHeader>

        {success ? (
          <div className="py-8 text-center">
            <div className="w-16 h-16 mx-auto mb-4 bg-green-100 dark:bg-green-900/20 rounded-full flex items-center justify-center">
              <Play className="h-8 w-8 text-green-600 dark:text-green-400" />
            </div>
            <h3 className="text-lg font-semibold mb-2">Server Deployed Successfully!</h3>
            <p className="text-sm text-muted-foreground">
              {server.server.name} is now running
            </p>
          </div>
        ) : (
          <div className="space-y-6">
            {/* Server Info */}
            <div className="space-y-2">
              <Label>Server</Label>
              <div className="p-3 bg-muted rounded-lg">
                <div className="font-medium">{server.server.title || server.server.name}</div>
                <div className="text-sm text-muted-foreground">{server.server.name}</div>
                <div className="text-xs text-muted-foreground mt-1">Version: {server.server.version}</div>
              </div>
            </div>

            <Separator />

            {/* Configuration */}
            <div className="space-y-4">
              <div>
                <Label className="text-base">Configuration (Optional)</Label>
                <p className="text-sm text-muted-foreground mt-1">
                  Add environment variables, arguments, or headers. Use prefixes: ARG_ for arguments, HEADER_ for headers.
                </p>
              </div>

              {/* Existing config */}
              {Object.keys(config).length > 0 && (
                <div className="space-y-2">
                  {Object.entries(config).map(([key, value]) => (
                    <div key={key} className="flex items-center gap-2 p-2 bg-muted rounded">
                      <div className="flex-1">
                        <div className="text-sm font-medium">{key}</div>
                        <div className="text-xs text-muted-foreground truncate">{value}</div>
                      </div>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-8 w-8"
                        onClick={() => handleRemoveConfig(key)}
                      >
                        <X className="h-4 w-4" />
                      </Button>
                    </div>
                  ))}
                </div>
              )}

              {/* Add new config */}
              <div className="space-y-2">
                <div className="grid grid-cols-2 gap-2">
                  <div>
                    <Input
                      placeholder="Key (e.g., API_KEY)"
                      value={newKey}
                      onChange={(e) => setNewKey(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter" && newKey && newValue) {
                          handleAddConfig()
                        }
                      }}
                    />
                  </div>
                  <div>
                    <Input
                      placeholder="Value"
                      value={newValue}
                      onChange={(e) => setNewValue(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter" && newKey && newValue) {
                          handleAddConfig()
                        }
                      }}
                    />
                  </div>
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  className="w-full"
                  onClick={handleAddConfig}
                  disabled={!newKey.trim() || !newValue.trim()}
                >
                  <Plus className="h-4 w-4 mr-2" />
                  Add Configuration
                </Button>
              </div>
            </div>

            {error && (
              <div className="p-3 bg-destructive/10 border border-destructive/20 rounded-lg">
                <p className="text-sm text-destructive">{error}</p>
              </div>
            )}

            {/* Actions */}
            <div className="flex justify-end gap-3">
              <Button variant="outline" onClick={handleClose} disabled={deploying}>
                Cancel
              </Button>
              <Button onClick={handleDeploy} disabled={deploying}>
                {deploying ? (
                  <>
                    <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                    Deploying...
                  </>
                ) : (
                  <>
                    <Play className="h-4 w-4 mr-2" />
                    Deploy
                  </>
                )}
              </Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}

