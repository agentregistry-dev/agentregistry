"use client"

import { useState } from "react"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { createServerV0, type ServerJson } from "@/lib/admin-api"
import { isValidDNSSubdomain } from "@/lib/validators"
import { Loader2, AlertCircle, Plus, Trash2 } from "lucide-react"
import { toast } from "sonner"

// Upstream MCP catalogue name — alphanumeric plus `.`, `_`, `-`, `/`.
// Accepts single-segment (`my-mcp`) and namespace/name (`io.example/foo`) forms.
const UPSTREAM_MCP_PACKAGE_NAME_RE = /^[a-zA-Z0-9._/-]+$/

// isValidMCPPackageName checks if the MCP package's serverName is valid.
// Every supported origin type (npm / pypi / oci) carries a per-type
// serverName and the server-side validator requires it.
function isValidMCPPackageName(s: string): boolean {
  return s.length >= 1 && s.length <= 200 && UPSTREAM_MCP_PACKAGE_NAME_RE.test(s)
}

type OriginType = 'npm' | 'pypi' | 'oci'

interface AddServerDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onServerAdded: () => void
}

export function AddServerDialog({ open, onOpenChange, onServerAdded }: AddServerDialogProps) {
  const [loading, setLoading] = useState(false)

  // Form fields
  const [schema, setSchema] = useState("2025-10-17")
  const [name, setName] = useState("")
  const [title, setTitle] = useState("")
  const [description, setDescription] = useState("")
  const [tag, setTag] = useState("latest")
  const [repositoryUrl, setRepositoryUrl] = useState("")

  // Schema collapsed to a single package per server. The dialog mirrors
  // the new polymorphic MCPPackage shape: origin.type drives which
  // per-type sub-object (npm / pypi / oci) is populated. `version` is
  // ignored for OCI (the version lives in the image ref's tag).
  type PackageDraft = { identifier: string; version: string; originType: OriginType; transport: string; port: string; serverName: string }
  const [pkg, setPkg] = useState<PackageDraft | null>(null)

  const resetForm = () => {
    setSchema("2025-10-17")
    setName("")
    setTitle("")
    setDescription("")
    setTag("latest")
    setRepositoryUrl("")
    setPkg(null)
  }

  const handleSubmit = async () => {
    setLoading(true)

    try {
      // Validate required fields
      if (!name.trim()) {
        throw new Error("Server name is required")
      }
      if (!isValidDNSSubdomain(name.trim())) {
        throw new Error("Server name must be DNS-1123 subdomain: lowercase alphanumeric, hyphens, and dots; max 253 chars; each dot-separated segment must start and end with alphanumeric")
      }
      if (pkg && !isValidMCPPackageName(pkg.serverName.trim())) {
        throw new Error("Upstream catalogue name is required (1-200 chars; alphanumeric plus '.', '_', '-', '/')")
      }

      if (!tag.trim()) {
        throw new Error("Tag is required")
      }
      if (!description.trim()) {
        throw new Error("Description is required")
      }

      // Build server object
      const server: ServerJson = {
        $schema: schema.trim(),
        name: name.trim(),
        description: description.trim(),
        tag: tag.trim(),
      }

      if (title.trim()) {
        server.title = title.trim()
      }

      const source: NonNullable<ServerJson['source']> = {}
      if (repositoryUrl.trim()) {
        source.repository = {
          url: repositoryUrl.trim(),
        }
      }
      // OCI carries its version in the image ref tag (identifier); npm
      // and pypi need a separate version. Validate accordingly.
      if (pkg && pkg.identifier.trim() && (pkg.originType === 'oci' || pkg.version.trim())) {
        const identifier = pkg.identifier.trim()
        const serverName = pkg.serverName.trim()
        const origin: NonNullable<NonNullable<typeof source.package>['origin']> = {
          type: pkg.originType,
          identifier,
        }
        if (pkg.originType === 'npm') {
          origin.npm = { version: pkg.version.trim(), serverName }
        } else if (pkg.originType === 'pypi') {
          origin.pypi = { version: pkg.version.trim(), serverName }
        } else {
          origin.oci = { serverName }
        }
        const transport: NonNullable<NonNullable<typeof source.package>['transport']> = {
          type: pkg.transport || 'stdio',
        }
        if (transport.type === 'http') {
          const port = parseInt(pkg.port, 10)
          if (!Number.isFinite(port) || port <= 0 || port > 65535) {
            throw new Error('http transport requires a port between 1 and 65535')
          }
          transport.port = port
        }
        source.package = {
          origin,
          transport,
        }
      }
      if (source.repository || source.package) {
        server.source = source
      }

      // Create server
      const { data } = await createServerV0({ body: server, throwOnError: true })

      // Show success toast
      toast.success(`Server "${data?.server.name}" created successfully!`)

      // Close dialog and refresh
      onOpenChange(false)
      onServerAdded()
      resetForm()
    } catch (err) {
      // Show error toast
      toast.error(err instanceof Error ? err.message : "Failed to create server")
    } finally {
      setLoading(false)
    }
  }

  const addPackage = () => {
    setPkg({ identifier: "", version: "", originType: "npm", transport: "stdio", port: "3000", serverName: "" })
  }

  const removePackage = () => {
    setPkg(null)
  }

  const updatePackage = (field: keyof PackageDraft, value: string) => {
    setPkg(prev => {
      if (!prev) return prev
      if (field === 'originType') {
        const originType = value as OriginType
        // OCI carries the version in the image ref tag — clear any
        // separately-entered version so the form doesn't carry stale state.
        return { ...prev, originType, version: originType === 'oci' ? '' : prev.version }
      }
      return { ...prev, [field]: value }
    })
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-6xl max-h-[90vh] overflow-y-auto px-8">
        <DialogHeader>
          <DialogTitle>Add New MCP Server</DialogTitle>
          <DialogDescription>
            Manually add a new MCP server to your registry
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-4">
          {/* Basic Information */}
          <div className="grid grid-cols-3 gap-4">
            <div className="space-y-2">
              <Label htmlFor="name">Server Name *</Label>
              <Input
                id="name"
                placeholder="my-server"
                value={name}
                onChange={(e) => setName(e.target.value)}
                disabled={loading}
                className={name && !isValidDNSSubdomain(name) ? "border-yellow-500" : ""}
              />
              <p className={`text-xs flex items-center gap-1 min-h-[1.25rem] ${name && !isValidDNSSubdomain(name) ? 'text-yellow-600' : 'invisible'}`}>
                <AlertCircle className="h-3 w-3" />
                Lowercase alphanumeric, hyphens, and dots. Max 253 chars. (e.g., my-server, io.example.app)
              </p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="title">Display Title</Label>
              <Input
                id="title"
                placeholder="My Server"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                disabled={loading}
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="tag">Tag *</Label>
              <Input
                id="tag"
                placeholder="latest"
                value={tag}
                onChange={(e) => setTag(e.target.value)}
                disabled={loading}
              />
            </div>
          </div>

          <div className="space-y-2">
            <Label htmlFor="description">Description *</Label>
            <Textarea
              id="description"
              placeholder="Describe what this server does..."
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={3}
              disabled={loading}
            />
          </div>

          <div className="grid grid-cols-1 gap-4">
            <div className="space-y-2">
              <Label htmlFor="repositoryUrl">Repository URL</Label>
              <div className="flex gap-2">
                <Input
                  id="repositoryUrl"
                  placeholder="https://github.com/user/repo"
                  value={repositoryUrl}
                  onChange={(e) => setRepositoryUrl(e.target.value)}
                  disabled={loading}
                  className="flex-1"
                />
              </div>
            </div>
          </div>

          {/* Package — only one is published per MCPServer. */}
          <div className="space-y-4 p-4 border rounded-lg">
            <div className="flex items-center justify-between">
              <h3 className="font-semibold text-sm">Package</h3>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={addPackage}
                disabled={loading || pkg !== null}
              >
                <Plus className="h-4 w-4 mr-1" />
                Add Package
              </Button>
            </div>

            {pkg ? (
              <div className="space-y-2 p-3 border rounded-md">
                <div className="flex gap-2 items-start">
                  <Input
                    placeholder="Package identifier"
                    value={pkg.identifier}
                    onChange={(e) => updatePackage("identifier", e.target.value)}
                    disabled={loading}
                    className="flex-1"
                  />
                  <Input
                    placeholder={pkg.originType === 'oci' ? 'Version (in image tag)' : 'Version'}
                    value={pkg.version}
                    onChange={(e) => updatePackage("version", e.target.value)}
                    disabled={loading || pkg.originType === 'oci'}
                    className="w-32"
                  />
                  <select
                    value={pkg.originType}
                    onChange={(e) => updatePackage("originType", e.target.value)}
                    className="px-3 py-2 border rounded-md bg-background text-foreground border-input focus:outline-none focus:ring-2 focus:ring-ring"
                    disabled={loading}
                  >
                    <option value="npm">npm</option>
                    <option value="pypi">pypi</option>
                    <option value="oci">oci</option>
                  </select>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    onClick={removePackage}
                    disabled={loading}
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </div>
                <div className="flex gap-3 items-center pl-2">
                  <Label className="text-sm text-muted-foreground">Transport *:</Label>
                  {["stdio", "http"].map((transport) => (
                    <label key={transport} className="flex items-center gap-1.5 cursor-pointer">
                      <input
                        type="radio"
                        name="package-transport"
                        checked={pkg.transport === transport}
                        onChange={() => updatePackage("transport", transport)}
                        disabled={loading}
                        className="border-gray-300"
                      />
                      <span className="text-sm">{transport}</span>
                    </label>
                  ))}
                  {pkg.transport === "http" && (
                    <>
                      <Label htmlFor="package-port" className="text-sm text-muted-foreground ml-2">
                        Port *:
                      </Label>
                      <Input
                        id="package-port"
                        type="number"
                        min={1}
                        max={65535}
                        value={pkg.port}
                        onChange={(e) => updatePackage("port", e.target.value)}
                        disabled={loading}
                        className="w-24"
                      />
                    </>
                  )}
                </div>
                {/* Required for every origin type (npm / pypi / oci).
                    Value must match the identity the publisher embedded in
                    the upstream artifact (npm mcpName / PyPI mcp-name /
                    OCI io.modelcontextprotocol.server.name). */}
                <div className="pl-2">
                  <Label htmlFor="serverName" className="text-sm text-muted-foreground">
                    Upstream catalogue name (mcpName) *
                  </Label>
                  <Input
                    id="serverName"
                    placeholder="io.github.user/server"
                    value={pkg.serverName}
                    onChange={(e) => updatePackage("serverName", e.target.value)}
                    disabled={loading}
                    className={!isValidMCPPackageName(pkg.serverName) ? "border-yellow-500" : ""}
                  />
                  <p className={`text-xs flex items-center gap-1 min-h-[1.25rem] ${!isValidMCPPackageName(pkg.serverName) ? 'text-yellow-600' : 'text-muted-foreground'}`}>
                    {!isValidMCPPackageName(pkg.serverName) ? (
                      <><AlertCircle className="h-3 w-3" /> 1-200 chars; alphanumeric plus `.`, `_`, `-`, `/`.</>
                    ) : (
                      <>Must match the identity embedded in the upstream package.</>
                    )}
                  </p>
                </div>
              </div>
            ) : (
              <p className="text-sm text-muted-foreground text-center py-2">
                No package added
              </p>
            )}
          </div>

        </div>

        <div className="flex justify-end gap-2">
          <Button
            variant="outline"
            onClick={() => {
              onOpenChange(false)
              resetForm()
            }}
            disabled={loading}
          >
            Cancel
          </Button>
          <Button
            onClick={handleSubmit}
            disabled={loading || !name.trim() || !tag.trim() || !description.trim()}
          >
            {loading && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            Create Server
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
