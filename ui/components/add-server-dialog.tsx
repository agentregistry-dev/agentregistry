"use client"

import { useState } from "react"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { adminApiClient, ServerJSON } from "@/lib/admin-api"
import { Loader2, AlertCircle, Plus, Trash2 } from "lucide-react"
import { toast } from "sonner"

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
  const [version, setVersion] = useState("")
  const [websiteUrl, setWebsiteUrl] = useState("")
  const [repositorySource, setRepositorySource] = useState<"github" | "gitlab" | "bitbucket">("github")
  const [repositoryUrl, setRepositoryUrl] = useState("")

  // Dynamic fields
  const [packages, setPackages] = useState<Array<{ identifier: string; version: string; registryType: string; transport: string }>>([])
  const [remotes, setRemotes] = useState<Array<{ type: string; url: string }>>([])

  const resetForm = () => {
    setSchema("2025-10-17")
    setName("")
    setTitle("")
    setDescription("")
    setVersion("")
    setWebsiteUrl("")
    setRepositoryUrl("")
    setPackages([])
    setRemotes([])
  }

  const handleSubmit = async () => {
    setLoading(true)

    try {
      // Validate required fields
      if (!name.trim()) {
        throw new Error("Server name is required")
      }
      
      // Validate name format (namespace/name)
      const namePattern = /^[a-zA-Z0-9.-]+\/[a-zA-Z0-9._-]+$/
      if (!namePattern.test(name.trim())) {
        throw new Error("Server name must be in format 'namespace/name' (e.g., 'io.example/my-server')")
      }
      
      if (!version.trim()) {
        throw new Error("Version is required")
      }
      if (!description.trim()) {
        throw new Error("Description is required")
      }

      // Build server object
      const server: ServerJSON = {
        $schema: schema.trim(),
        name: name.trim(),
        description: description.trim(),
        version: version.trim(),
      }

      if (title.trim()) {
        server.title = title.trim()
      }

      if (websiteUrl.trim()) {
        server.websiteUrl = websiteUrl.trim()
      }

      if (repositoryUrl.trim()) {
        server.repository = {
          source: repositorySource,
          url: repositoryUrl.trim(),
        }
      }

      if (packages.length > 0) {
        server.packages = packages
          .filter(p => p.identifier.trim() && p.version.trim())
          .map(p => ({
            identifier: p.identifier.trim(),
            version: p.version.trim(),
            registryType: p.registryType as 'npm' | 'pypi' | 'docker',
            transport: { type: p.transport || 'stdio' },
          }))
      }

      if (remotes.length > 0) {
        server.remotes = remotes
          .filter(r => r.type.trim())
          .map(r => ({
            type: r.type.trim(),
            url: r.url.trim() || undefined,
          }))
      }

      // Create server
      const result = await adminApiClient.createServer(server)
      
      // Show success toast
      toast.success(`Server "${result.server.name}" created successfully!`)

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
    setPackages([...packages, { identifier: "", version: "", registryType: "npm", transport: "stdio" }])
  }

  const removePackage = (index: number) => {
    setPackages(packages.filter((_, i) => i !== index))
  }

  const updatePackage = (index: number, field: string, value: string) => {
    const updated = [...packages]
    updated[index] = { ...updated[index], [field]: value }
    setPackages(updated)
  }

  const addRemote = () => {
    setRemotes([...remotes, { type: "sse", url: "" }])
  }

  const removeRemote = (index: number) => {
    setRemotes(remotes.filter((_, i) => i !== index))
  }

  const updateRemote = (index: number, field: string, value: string) => {
    const updated = [...remotes]
    updated[index] = { ...updated[index], [field]: value }
    setRemotes(updated)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-6xl max-h-[90vh] overflow-y-auto">
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
                placeholder="io.example/my-server"
                value={name}
                onChange={(e) => setName(e.target.value)}
                disabled={loading}
                className={name && !/^[a-zA-Z0-9.-]+\/[a-zA-Z0-9._-]+$/.test(name) ? "border-yellow-500" : ""}
              />
              <p className={`text-xs flex items-center gap-1 min-h-[1.25rem] ${name && !/^[a-zA-Z0-9.-]+\/[a-zA-Z0-9._-]+$/.test(name) ? 'text-yellow-600' : 'invisible'}`}>
                <AlertCircle className="h-3 w-3" />
                Must be in format namespace/name (e.g., io.example/my-server)
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
              <Label htmlFor="version">Version *</Label>
              <Input
                id="version"
                placeholder="1.0.0"
                value={version}
                onChange={(e) => setVersion(e.target.value)}
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

          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <Label htmlFor="websiteUrl">Website URL</Label>
              <Input
                id="websiteUrl"
                placeholder="https://example.com"
                value={websiteUrl}
                onChange={(e) => setWebsiteUrl(e.target.value)}
                disabled={loading}
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="repositoryUrl">Repository URL</Label>
              <div className="flex gap-2">
                <select
                  value={repositorySource}
                  onChange={(e) => setRepositorySource(e.target.value as any)}
                  className="px-3 py-2 border rounded-md bg-background text-foreground border-input focus:outline-none focus:ring-2 focus:ring-ring"
                  disabled={loading}
                >
                  <option value="github">GitHub</option>
                  <option value="gitlab">GitLab</option>
                  <option value="bitbucket">Bitbucket</option>
                </select>
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

          {/* Packages */}
          <div className="space-y-4 p-4 border rounded-lg">
            <div className="flex items-center justify-between">
              <h3 className="font-semibold text-sm">Packages</h3>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={addPackage}
                disabled={loading}
              >
                <Plus className="h-4 w-4 mr-1" />
                Add Package
              </Button>
            </div>

            {packages.map((pkg, index) => (
              <div key={index} className="space-y-2 p-3 border rounded-md">
                <div className="flex gap-2 items-start">
                  <Input
                    placeholder="Package identifier"
                    value={pkg.identifier}
                    onChange={(e) => updatePackage(index, "identifier", e.target.value)}
                    disabled={loading}
                    className="flex-1"
                  />
                  <Input
                    placeholder="Version"
                    value={pkg.version}
                    onChange={(e) => updatePackage(index, "version", e.target.value)}
                    disabled={loading}
                    className="w-32"
                  />
                  <select
                    value={pkg.registryType}
                    onChange={(e) => updatePackage(index, "registryType", e.target.value)}
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
                    onClick={() => removePackage(index)}
                    disabled={loading}
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </div>
                <div className="flex gap-2 items-center pl-2">
                  <Label className="text-xs text-muted-foreground">Transport *:</Label>
                  {["stdio", "sse", "streamable-http"].map((transport) => (
                    <label key={transport} className="flex items-center gap-1 cursor-pointer">
                      <input
                        type="radio"
                        name={`transport-${index}`}
                        checked={pkg.transport === transport}
                        onChange={() => updatePackage(index, "transport", transport)}
                        disabled={loading}
                        className="border-gray-300"
                      />
                      <span className="text-xs">{transport}</span>
                    </label>
                  ))}
                </div>
              </div>
            ))}

            {packages.length === 0 && (
              <p className="text-sm text-muted-foreground text-center py-2">
                No packages added
              </p>
            )}
          </div>

          {/* Remotes */}
          <div className="space-y-4 p-4 border rounded-lg">
            <div className="flex items-center justify-between">
              <h3 className="font-semibold text-sm">Remotes</h3>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={addRemote}
                disabled={loading}
              >
                <Plus className="h-4 w-4 mr-1" />
                Add Remote
              </Button>
            </div>

            {remotes.map((remote, index) => (
              <div key={index} className="flex gap-2 items-start">
                <Input
                  placeholder="Type (e.g., sse, stdio)"
                  value={remote.type}
                  onChange={(e) => updateRemote(index, "type", e.target.value)}
                  disabled={loading}
                  className="w-40"
                />
                <Input
                  placeholder="URL (optional)"
                  value={remote.url}
                  onChange={(e) => updateRemote(index, "url", e.target.value)}
                  disabled={loading}
                  className="flex-1"
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  onClick={() => removeRemote(index)}
                  disabled={loading}
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              </div>
            ))}

            {remotes.length === 0 && (
              <p className="text-sm text-muted-foreground text-center py-2">
                No remotes added
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
            disabled={loading || !name.trim() || !version.trim() || !description.trim()}
          >
            {loading && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            Create Server
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}

