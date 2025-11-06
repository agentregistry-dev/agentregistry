"use client"

import { useState } from "react"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { adminApiClient, ServerJSON } from "@/lib/admin-api"
import { Loader2, CheckCircle2, XCircle, AlertCircle, Plus, Trash2 } from "lucide-react"

interface AddServerDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onServerAdded: () => void
}

export function AddServerDialog({ open, onOpenChange, onServerAdded }: AddServerDialogProps) {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState(false)

  // Form fields
  const [name, setName] = useState("")
  const [title, setTitle] = useState("")
  const [description, setDescription] = useState("")
  const [version, setVersion] = useState("")
  const [websiteUrl, setWebsiteUrl] = useState("")
  const [repositorySource, setRepositorySource] = useState<"github" | "gitlab" | "bitbucket">("github")
  const [repositoryUrl, setRepositoryUrl] = useState("")

  // Dynamic fields
  const [packages, setPackages] = useState<Array<{ identifier: string; version: string; registryType: string }>>([])
  const [remotes, setRemotes] = useState<Array<{ type: string; url: string }>>([])

  const resetForm = () => {
    setName("")
    setTitle("")
    setDescription("")
    setVersion("")
    setWebsiteUrl("")
    setRepositoryUrl("")
    setPackages([])
    setRemotes([])
    setError(null)
    setSuccess(false)
  }

  const handleSubmit = async () => {
    setLoading(true)
    setError(null)
    setSuccess(false)

    try {
      // Validate required fields
      if (!name.trim()) {
        throw new Error("Server name is required")
      }
      if (!version.trim()) {
        throw new Error("Version is required")
      }
      if (!description.trim()) {
        throw new Error("Description is required")
      }

      // Build server object
      const server: ServerJSON = {
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
      await adminApiClient.createServer(server)
      
      setSuccess(true)

      // Wait a bit to show success, then close and refresh
      setTimeout(() => {
        onOpenChange(false)
        onServerAdded()
        resetForm()
      }, 1500)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create server")
    } finally {
      setLoading(false)
    }
  }

  const addPackage = () => {
    setPackages([...packages, { identifier: "", version: "", registryType: "npm" }])
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
      <DialogContent className="max-w-3xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Add New MCP Server</DialogTitle>
          <DialogDescription>
            Manually add a new MCP server to your private registry
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-4">
          {/* Required Fields */}
          <div className="space-y-4 p-4 border rounded-lg">
            <h3 className="font-semibold text-sm">Required Information</h3>
            
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="name">Server Name *</Label>
                <Input
                  id="name"
                  placeholder="com.example/my-server"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
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
          </div>

          {/* Optional Fields */}
          <div className="space-y-4 p-4 border rounded-lg">
            <h3 className="font-semibold text-sm">Optional Information</h3>
            
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
                  // eslint-disable-next-line @typescript-eslint/no-explicit-any
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
              <div key={index} className="flex gap-2 items-start">
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

          {/* Error/Success Messages */}
          {error && (
            <div className="p-4 rounded-lg border bg-red-50 border-red-200 dark:bg-red-950 dark:border-red-800">
              <div className="flex items-start gap-3">
                <XCircle className="h-5 w-5 text-red-600 dark:text-red-400 mt-0.5" />
                <div className="flex-1">
                  <p className="font-medium text-red-900 dark:text-red-100">
                    {error}
                  </p>
                </div>
              </div>
            </div>
          )}

          {success && (
            <div className="p-4 rounded-lg border bg-green-50 border-green-200 dark:bg-green-950 dark:border-green-800">
              <div className="flex items-start gap-3">
                <CheckCircle2 className="h-5 w-5 text-green-600 dark:text-green-400 mt-0.5" />
                <div className="flex-1">
                  <p className="font-medium text-green-900 dark:text-green-100">
                    Server created successfully!
                  </p>
                </div>
              </div>
            </div>
          )}

          <div className="flex items-center gap-3 p-3 rounded-lg bg-blue-50 border border-blue-200 dark:bg-blue-950 dark:border-blue-800">
            <AlertCircle className="h-5 w-5 text-blue-600 dark:text-blue-400" />
            <div className="text-sm text-blue-900 dark:text-blue-100">
              <p>This will create a new server entry in your private registry and make it available via the API.</p>
            </div>
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

