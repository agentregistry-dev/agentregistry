"use client"

import { useState } from "react"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { createPromptV0, type PromptJson } from "@/lib/admin-api"

interface AddPromptDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onPromptAdded: () => void
}

export function AddPromptDialog({ open, onOpenChange, onPromptAdded }: AddPromptDialogProps) {
  const [name, setName] = useState("")
  const [description, setDescription] = useState("")
  const [version, setVersion] = useState("1.0.0")
  const [content, setContent] = useState("")
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    setLoading(true)

    try {
      if (!name.trim()) {
        throw new Error("Prompt name is required")
      }
      if (!version.trim()) {
        throw new Error("Version is required")
      }
      if (!content.trim()) {
        throw new Error("Prompt content is required")
      }

      const promptData: PromptJson = {
        name: name.trim(),
        version: version.trim(),
        content: content.trim(),
        description: description.trim() || undefined,
      }

      await createPromptV0({ body: promptData, throwOnError: true })

      // Reset form
      setName("")
      setDescription("")
      setVersion("1.0.0")
      setContent("")

      // Notify parent and close dialog
      onPromptAdded()
      onOpenChange(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to add prompt")
    } finally {
      setLoading(false)
    }
  }

  const handleCancel = () => {
    setName("")
    setDescription("")
    setVersion("1.0.0")
    setContent("")
    setError(null)
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[80vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Add Prompt</DialogTitle>
          <DialogDescription>
            Add a new prompt to the registry
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4 py-4">
          <div className="space-y-2">
            <Label htmlFor="name">
              Prompt Name <span className="text-red-500">*</span>
            </Label>
            <Input
              id="name"
              placeholder="my-prompt"
              value={name}
              onChange={(e) => setName(e.target.value)}
              disabled={loading}
              required
            />
            <p className="text-xs text-muted-foreground">
              Use lowercase alphanumeric characters, hyphens, and underscores only
            </p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="description">
              Description
            </Label>
            <Input
              id="description"
              placeholder="A brief description of what this prompt does"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              disabled={loading}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="version">
              Version <span className="text-red-500">*</span>
            </Label>
            <Input
              id="version"
              placeholder="1.0.0"
              value={version}
              onChange={(e) => setVersion(e.target.value)}
              disabled={loading}
              required
            />
            <p className="text-xs text-muted-foreground">
              e.g., &quot;1.0.0&quot;, &quot;2.3.1&quot;
            </p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="content">
              Prompt Content <span className="text-red-500">*</span>
            </Label>
            <Textarea
              id="content"
              placeholder="Enter the prompt content..."
              rows={8}
              value={content}
              onChange={(e) => setContent(e.target.value)}
              disabled={loading}
              required
              className="font-mono text-sm"
            />
          </div>

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
              {loading ? "Adding..." : "Add Prompt"}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
