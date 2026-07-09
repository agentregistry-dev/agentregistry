"use client"

import { useState } from "react"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { createPluginV0, type PluginJson } from "@/lib/admin-api"
import { isValidDNSSubdomain, DNS_SUBDOMAIN_HELP } from "@/lib/validators"

interface AddPluginDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onPluginAdded: () => void
}

export function AddPluginDialog({ open, onOpenChange, onPluginAdded }: AddPluginDialogProps) {
  const [name, setName] = useState("")
  const [title, setTitle] = useState("")
  const [description, setDescription] = useState("")
  const [tag, setTag] = useState("latest")
  const [harnesses, setHarnesses] = useState("")
  const [repositoryUrl, setRepositoryUrl] = useState("")
  const [branch, setBranch] = useState("")
  const [commit, setCommit] = useState("")
  const [subfolder, setSubfolder] = useState("")
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const resetForm = () => {
    setName("")
    setTitle("")
    setDescription("")
    setTag("latest")
    setHarnesses("")
    setRepositoryUrl("")
    setBranch("")
    setCommit("")
    setSubfolder("")
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    setLoading(true)

    try {
      // Validate required fields
      if (!name.trim()) {
        throw new Error("Plugin name is required")
      }
      if (!isValidDNSSubdomain(name.trim())) {
        throw new Error("Plugin name must be DNS-1123 subdomain: lowercase alphanumeric, hyphens, and dots; max 253 chars; each dot-separated segment must start and end with alphanumeric")
      }
      if (!tag.trim()) {
        throw new Error("Tag is required")
      }
      const trimmedRepositoryUrl = repositoryUrl.trim()
      if (!trimmedRepositoryUrl) {
        throw new Error("Repository URL is required")
      }

      const harnessList = harnesses
        .split(",")
        .map((h) => h.trim())
        .filter(Boolean)

      // Construct the PluginJSON object. Spec is user intent only — the
      // Plugin controller resolves the source pointer to a pinned
      // commit/digest and writes the manifest + inventory into status.
      const pluginData: PluginJson = {
        name: name.trim(),
        tag: tag.trim(),
        ...(title.trim() ? { title: title.trim() } : {}),
        ...(description.trim() ? { description: description.trim() } : {}),
        ...(harnessList.length > 0 ? { harnesses: harnessList } : {}),
        source: {
          type: "git",
          git: {
            repository: {
              url: trimmedRepositoryUrl,
              ...(branch.trim() ? { branch: branch.trim() } : {}),
              ...(commit.trim() ? { commit: commit.trim() } : {}),
              ...(subfolder.trim() ? { subfolder: subfolder.trim() } : {}),
            },
          },
        },
      }

      // Create the plugin
      await createPluginV0({ body: pluginData, throwOnError: true })

      resetForm()

      // Notify parent and close dialog
      onPluginAdded()
      onOpenChange(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to add plugin")
    } finally {
      setLoading(false)
    }
  }

  const handleCancel = () => {
    resetForm()
    setError(null)
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[80vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Add Plugin</DialogTitle>
          <DialogDescription>
            Register a plugin bundle from a git source. The registry hosts nothing —
            the controller resolves the source to a pinned commit, then scans it for
            the plugin manifest and inventory (skills, commands, agents, hooks).
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4 py-4">
          <div className="space-y-2">
            <Label htmlFor="plugin-name">
              Plugin Name <span className="text-red-500">*</span>
            </Label>
            <Input
              id="plugin-name"
              placeholder="my-plugin"
              value={name}
              onChange={(e) => setName(e.target.value)}
              disabled={loading}
              required
            />
            <p className="text-xs text-muted-foreground">{DNS_SUBDOMAIN_HELP}</p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="plugin-title">Title</Label>
            <Input
              id="plugin-title"
              placeholder="My Plugin"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              disabled={loading}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="plugin-description">Description</Label>
            <Textarea
              id="plugin-description"
              placeholder="A description of what this plugin provides"
              rows={3}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              disabled={loading}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="plugin-tag">
              Tag <span className="text-red-500">*</span>
            </Label>
            <Input
              id="plugin-tag"
              placeholder="latest"
              value={tag}
              onChange={(e) => setTag(e.target.value)}
              disabled={loading}
              required
            />
            <p className="text-xs text-muted-foreground">
              e.g., &quot;latest&quot;, &quot;1.0.0&quot;, &quot;v2.3.1&quot;
            </p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="plugin-harnesses">Harnesses</Label>
            <Input
              id="plugin-harnesses"
              placeholder="claude-code, codex"
              value={harnesses}
              onChange={(e) => setHarnesses(e.target.value)}
              disabled={loading}
            />
            <p className="text-xs text-muted-foreground">
              Comma-separated harness formats this bundle carries native manifests for
            </p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="plugin-repository-url">
              Repository URL <span className="text-red-500">*</span>
            </Label>
            <Input
              id="plugin-repository-url"
              placeholder="https://github.com/username/repo"
              value={repositoryUrl}
              onChange={(e) => setRepositoryUrl(e.target.value)}
              disabled={loading}
              type="url"
              required
            />
            <p className="text-xs text-muted-foreground">
              Link to the plugin&apos;s Git repository (GitHub, GitLab, Bitbucket, etc.)
            </p>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <Label htmlFor="plugin-branch">Branch</Label>
              <Input
                id="plugin-branch"
                placeholder="main"
                value={branch}
                onChange={(e) => setBranch(e.target.value)}
                disabled={loading}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="plugin-commit">Commit</Label>
              <Input
                id="plugin-commit"
                placeholder="abc1234…"
                value={commit}
                onChange={(e) => setCommit(e.target.value)}
                disabled={loading}
              />
            </div>
          </div>

          <div className="space-y-2">
            <Label htmlFor="plugin-subfolder">Subfolder</Label>
            <Input
              id="plugin-subfolder"
              placeholder="plugins/my-plugin"
              value={subfolder}
              onChange={(e) => setSubfolder(e.target.value)}
              disabled={loading}
            />
            <p className="text-xs text-muted-foreground">
              Path to the plugin inside a monorepo (optional)
            </p>
          </div>

          <p className="text-xs text-muted-foreground rounded-md bg-muted p-3">
            Branch and commit are optional — if omitted, the remote default branch is
            used. Whatever ref you supply, the controller resolves it to a concrete
            commit SHA and pins it in the plugin&apos;s status so the published tag is
            reproducible.
          </p>

          {error && (
            <div className="rounded-md bg-red-50 p-3 text-sm text-red-800">
              {error}
            </div>
          )}

          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={handleCancel} disabled={loading}>
              Cancel
            </Button>
            <Button type="submit" disabled={loading}>
              {loading ? "Adding..." : "Add Plugin"}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
