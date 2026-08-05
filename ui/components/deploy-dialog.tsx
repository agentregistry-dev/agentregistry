"use client"

import { useState, useRef } from "react"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { deployServer as deployServerApi, type ServerResponse, type AgentResponse } from "@/lib/admin-api"
import { Play, Plus, X, Loader2, CheckCircle } from "lucide-react"

type DeployResourceType = "mcp" | "agent"

interface DeployDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  resourceType: DeployResourceType
  server?: ServerResponse | null
  agent?: AgentResponse | null
  onDeploySuccess?: () => void
}

const runtimeId = "kubernetes-default"

export function DeployDialog({ open, onOpenChange, resourceType, server, agent, onDeploySuccess }: DeployDialogProps) {
  const [deploying, setDeploying] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState(false)
  const [config, setConfig] = useState<Record<string, string>>({})
  const [newKey, setNewKey] = useState("")
  const [newValue, setNewValue] = useState("")
  const [namespace, setNamespace] = useState("")
  const keyInputRef = useRef<HTMLInputElement>(null)

  const name = resourceType === "mcp"
    ? server?.server.name
    : agent?.agent.name

  const tag = resourceType === "mcp"
    ? server?.server.tag
    : agent?.agent.tag

  const displayName = resourceType === "mcp"
    ? (server?.server.title || server?.server.name)
    : agent?.agent.name

  const handleAddConfig = () => {
    if (newKey.trim() && newValue.trim()) {
      setConfig({ ...config, [newKey.trim()]: newValue.trim() })
      setNewKey("")
      setNewValue("")
      keyInputRef.current?.focus()
    }
  }

  const handleRemoveConfig = (key: string) => {
    const newConfig = { ...config }
    delete newConfig[key]
    setConfig(newConfig)
  }

  const handleDeploy = async () => {
    if (!name || !tag) return

    try {
      setDeploying(true)
      setError(null)

      const env: Record<string, string> = { ...config }
      env["KAGENT_NAMESPACE"] = namespace || "default"

      await deployServerApi({
        body: {
          serverName: name,
          tag,
          env,
          runtimeId,
          resourceType,
        },
        throwOnError: true,
      })

      setSuccess(true)
      setTimeout(() => {
        onOpenChange(false)
        resetState()
        onDeploySuccess?.()
      }, 1500)
    } catch (err) {
      setError(err instanceof Error ? err.message : `Failed to deploy ${resourceType === "mcp" ? "server" : "agent"}`)
    } finally {
      setDeploying(false)
    }
  }

  const resetState = () => {
    setError(null)
    setSuccess(false)
    setConfig({})
    setNewKey("")
    setNewValue("")
    setNamespace("")
  }

  const handleClose = () => {
    if (!deploying) {
      onOpenChange(false)
      resetState()
    }
  }

  if (resourceType === "mcp" && !server) return null
  if (resourceType === "agent" && !agent) return null

  const resourceLabel = resourceType === "mcp" ? "Server" : "Agent"
  const envCount = Object.keys(config).length

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-w-lg max-h-[80vh] overflow-y-auto overscroll-contain">
        <DialogHeader>
          <DialogTitle className="text-lg">
            Deploy {displayName}
          </DialogTitle>
          <DialogDescription>
            <span className="font-mono text-xs">{name}</span>
            <span className="mx-1.5 text-muted-foreground/40">&middot;</span>
            <span className="text-xs">{tag}</span>
            <span className="mx-1.5 text-muted-foreground/40">&middot;</span>
            <span className="text-xs capitalize">{resourceLabel}</span>
          </DialogDescription>
        </DialogHeader>

        {success ? (
          <div className="py-6 text-center" role="status" aria-live="polite">
            <CheckCircle className="h-10 w-10 mx-auto mb-3 text-green-600 dark:text-green-400" aria-hidden="true" />
            <p className="text-sm font-medium">
              Deployment started for {displayName}
            </p>
            <p className="text-xs text-muted-foreground mt-1">
              Check the Deployed tab for status
            </p>
          </div>
        ) : (
          <form
            className="space-y-5"
            onSubmit={(e) => { e.preventDefault(); handleDeploy() }}
          >
            {/* Kubernetes Namespace */}
            <div className="space-y-2">
              <Label htmlFor="deploy-namespace">Kubernetes Namespace</Label>
              <Input
                id="deploy-namespace"
                name="namespace"
                placeholder="default"
                value={namespace}
                onChange={(e) => setNamespace(e.target.value)}
                autoComplete="off"
                spellCheck={false}
              />
              <p className="text-xs text-muted-foreground">
                Leave empty for the default namespace.
              </p>
            </div>

            {/* Environment Variables */}
            <fieldset className="space-y-3">
              <div>
                <Label className="text-sm">Environment Variables</Label>
                <p className="text-xs text-muted-foreground mt-0.5">
                  {resourceType === "mcp"
                    ? "Use ARG_ prefix for runtime arguments, HEADER_ for HTTP headers."
                    : "Set API keys and configuration for the agent."}
                </p>
              </div>

              {envCount > 0 && (
                <ul className="space-y-1.5" aria-label="Environment variables">
                  {Object.entries(config).map(([key, value]) => (
                    <li key={key} className="flex items-center gap-2 py-1.5 px-2.5 bg-muted/60 rounded-md text-sm">
                      <span className="font-mono font-medium text-xs min-w-0 truncate">{key}</span>
                      <span className="text-muted-foreground/40">=</span>
                      <span className="text-xs text-muted-foreground min-w-0 truncate flex-1">{value}</span>
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        className="h-6 w-6 shrink-0 text-muted-foreground hover:text-destructive"
                        onClick={() => handleRemoveConfig(key)}
                        aria-label={`Remove ${key}`}
                      >
                        <X className="h-3.5 w-3.5" aria-hidden="true" />
                      </Button>
                    </li>
                  ))}
                </ul>
              )}

              <div className="flex items-end gap-2">
                <div className="flex-1 space-y-1">
                  <Label htmlFor="env-key" className="sr-only">Variable name</Label>
                  <Input
                    ref={keyInputRef}
                    id="env-key"
                    name="env-key"
                    placeholder="KEY&hellip;"
                    value={newKey}
                    onChange={(e) => setNewKey(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" && newKey && newValue) {
                        e.preventDefault()
                        handleAddConfig()
                      }
                    }}
                    autoComplete="off"
                    spellCheck={false}
                    className="font-mono text-xs h-8"
                  />
                </div>
                <div className="flex-1 space-y-1">
                  <Label htmlFor="env-value" className="sr-only">Variable value</Label>
                  <Input
                    id="env-value"
                    name="env-value"
                    placeholder="value&hellip;"
                    value={newValue}
                    onChange={(e) => setNewValue(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" && newKey && newValue) {
                        e.preventDefault()
                        handleAddConfig()
                      }
                    }}
                    autoComplete="off"
                    spellCheck={false}
                    className="text-xs h-8"
                  />
                </div>
                <Button
                  type="button"
                  variant="outline"
                  size="icon"
                  className="h-8 w-8 shrink-0"
                  onClick={handleAddConfig}
                  disabled={!newKey.trim() || !newValue.trim()}
                  aria-label="Add variable"
                >
                  <Plus className="h-3.5 w-3.5" aria-hidden="true" />
                </Button>
              </div>
            </fieldset>

            {error && (
              <div className="p-3 bg-destructive/10 border border-destructive/20 rounded-md" role="alert">
                <p className="text-sm text-destructive">{error}</p>
                <p className="text-xs text-muted-foreground mt-1">
                  Check configuration and try again, or verify the runtime is reachable.
                </p>
              </div>
            )}

            {/* Actions */}
            <div className="flex justify-end gap-2 pt-1">
              <Button type="button" variant="ghost" onClick={handleClose} disabled={deploying}>
                Cancel
              </Button>
              <Button type="submit" disabled={deploying}>
                {deploying ? (
                  <>
                    <Loader2 className="h-4 w-4 mr-2 animate-spin motion-reduce:animate-none" aria-hidden="true" />
                    Deploying&hellip;
                  </>
                ) : (
                  <>
                    <Play className="h-3.5 w-3.5 mr-1.5" aria-hidden="true" />
                    Deploy {resourceLabel}
                  </>
                )}
              </Button>
            </div>
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}
