"use client"

import { useState } from "react"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { createServerV0, type ServerJson } from "@/lib/admin-api"
import { isValidDNSLabel } from "@/lib/validators"
import { Loader2, AlertCircle, Plus, Trash2 } from "lucide-react"
import { toast } from "sonner"

// Upstream MCP catalogue name (e.g. "io.github.user/server") — MCPServer-only.
const UPSTREAM_MCP_PACKAGE_NAME_RE = /^[a-zA-Z0-9.-]+\/[a-zA-Z0-9._-]+$/

// isValidMCPPackageName checks if an optional MCP package's mcpName is valid (NAMESPACE/NAME)
// Note: This NAMESPACE !== the registry resource namespace, and is a naming convention for MCPs
function isValidMCPPackageName(s: string): boolean {
  return s.length == 0 || (s.length >= 3 && s.length <= 200 && UPSTREAM_MCP_PACKAGE_NAME_RE.test(s))
}

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

  // Schema collapsed to a single package per server.
  type PackageDraft = { identifier: string; version: string; registryType: string; transport: string; mcpName: string }
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
      if (!isValidDNSLabel(name.trim())) {
        throw new Error("Server name must be DNS-1123 label: lowercase alphanumeric and hyphens, max 63 chars, start/end with alphanumeric")
      }
      if (!isValidMCPPackageName(pkg?.mcpName.trim() || "")) {
        throw new Error("Upstream catalogue name must be unset or in 'domain/name' shape (e.g. 'io.github.user/server')")
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
      if (pkg && pkg.identifier.trim() && pkg.version.trim()) {
        source.package = {
          identifier: pkg.identifier.trim(),
          version: pkg.version.trim(),
          registryType: pkg.registryType as 'npm' | 'pypi' | 'docker',
          transport: { type: pkg.transport || 'stdio' },
        }
        if (pkg.mcpName.trim()) {
          source.package.mcpName = pkg.mcpName.trim()
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
    setPkg({ identifier: "", version: "", registryType: "npm", transport: "stdio", mcpName: "" })
  }

  const removePackage = () => {
    setPkg(null)
  }

  const updatePackage = (field: keyof PackageDraft, value: string) => {
    setPkg(prev => (prev ? { ...prev, [field]: value } : prev))
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
                className={name && !isValidDNSLabel(name) ? "border-yellow-500" : ""}
              />
              <p className={`text-xs flex items-center gap-1 min-h-[1.25rem] ${name && !isValidDNSLabel(name) ? 'text-yellow-600' : 'invisible'}`}>
                <AlertCircle className="h-3 w-3" />
                Lowercase alphanumeric and hyphens only. Max 63 chars. (e.g., my-server)
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
                    placeholder="Version"
                    value={pkg.version}
                    onChange={(e) => updatePackage("version", e.target.value)}
                    disabled={loading}
                    className="w-32"
                  />
                  <select
                    value={pkg.registryType}
                    onChange={(e) => updatePackage("registryType", e.target.value)}
                    className="px-3 py-2 border rounded-md bg-background text-foreground border-input focus:outline-none focus:ring-2 focus:ring-ring"
                    disabled={loading}
                  >
                    <option value="npm">npm</option>
                    <option value="pypi">pypi</option>
                    <option value="docker">docker</option>
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
                  {["stdio", "sse", "streamable-http"].map((transport) => (
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
                </div>
                {/* Optional upstream catalogue identity. Required only when the package's published
                    mcpName/mcp-name/OCI label uses the upstream `namespace/name` shape that the
                    DNS-label Server Name can't represent. */}
                <div className="pl-2">
                  <Label htmlFor="mcpName" className="text-sm text-muted-foreground">
                    Upstream catalogue name (mcpName)
                  </Label>
                  <Input
                    id="mcpName"
                    placeholder="io.github.user/server"
                    value={pkg.mcpName}
                    onChange={(e) => updatePackage("mcpName", e.target.value)}
                    disabled={loading}
                    className={!isValidMCPPackageName(pkg.mcpName) ? "border-yellow-500" : ""}
                  />
                  <p className={`text-xs flex items-center gap-1 min-h-[1.25rem] ${!isValidMCPPackageName(pkg.mcpName) ? 'text-yellow-600' : 'text-muted-foreground'}`}>
                    {!isValidMCPPackageName(pkg.mcpName) ? (
                      <><AlertCircle className="h-3 w-3" /> Must match `namespace/name` shape.</>
                    ) : (
                      <>Optional. Ignored for MCPB packages.</>
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
