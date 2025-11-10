"use client"

import { useState } from "react"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { adminApiClient, SkillJSON } from "@/lib/admin-api"

interface AddSkillDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSkillAdded: () => void
}

export function AddSkillDialog({ open, onOpenChange, onSkillAdded }: AddSkillDialogProps) {
  const [name, setName] = useState("")
  const [description, setDescription] = useState("")
  const [version, setVersion] = useState("latest")
  const [repositoryUrl, setRepositoryUrl] = useState("")
  const [dockerImage, setDockerImage] = useState("")
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    setLoading(true)

    try {
      // Validate required fields
      if (!name.trim()) {
        throw new Error("Skill name is required")
      }
      if (!description.trim()) {
        throw new Error("Description is required")
      }
      if (!version.trim()) {
        throw new Error("Version is required")
      }
      if (!dockerImage.trim()) {
        throw new Error("Docker image is required")
      }

      // Construct the SkillJSON object
      const skillData: SkillJSON = {
        name: name.trim(),
        description: description.trim(),
        version: version.trim(),
        repository: repositoryUrl.trim() ? {
          url: repositoryUrl.trim(),
          source: "github"
        } : undefined,
        packages: [
          {
            registryType: "docker",
            identifier: dockerImage.trim(),
            version: version.trim(),
            transport: {
              type: "docker"
            }
          }
        ]
      }

      // Create the skill
      await adminApiClient.createSkill(skillData)

      // Reset form
      setName("")
      setDescription("")
      setVersion("latest")
      setRepositoryUrl("")
      setDockerImage("")

      // Notify parent and close dialog
      onSkillAdded()
      onOpenChange(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to add skill")
    } finally {
      setLoading(false)
    }
  }

  const handleCancel = () => {
    setName("")
    setDescription("")
    setVersion("latest")
    setRepositoryUrl("")
    setDockerImage("")
    setError(null)
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[80vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Add Skill</DialogTitle>
          <DialogDescription>
            Add a new skill to the registry
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4 py-4">
          <div className="space-y-2">
            <Label htmlFor="name">
              Skill Name <span className="text-red-500">*</span>
            </Label>
            <Input
              id="name"
              placeholder="my-skill"
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
              Description <span className="text-red-500">*</span>
            </Label>
            <Textarea
              id="description"
              placeholder="A description of what this skill does"
              rows={3}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              disabled={loading}
              required
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="version">
              Version <span className="text-red-500">*</span>
            </Label>
            <Input
              id="version"
              placeholder="latest"
              value={version}
              onChange={(e) => setVersion(e.target.value)}
              disabled={loading}
              required
            />
            <p className="text-xs text-muted-foreground">
              e.g., &quot;latest&quot;, &quot;1.0.0&quot;, &quot;v2.3.1&quot;
            </p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="repositoryUrl">
              GitHub Repository URL
            </Label>
            <Input
              id="repositoryUrl"
              placeholder="https://github.com/username/repo"
              value={repositoryUrl}
              onChange={(e) => setRepositoryUrl(e.target.value)}
              disabled={loading}
              type="url"
            />
            <p className="text-xs text-muted-foreground">
              Optional: Link to the skill&apos;s source code repository
            </p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="dockerImage">
              Docker Image <span className="text-red-500">*</span>
            </Label>
            <Input
              id="dockerImage"
              placeholder="docker.io/username/my-skill:latest"
              value={dockerImage}
              onChange={(e) => setDockerImage(e.target.value)}
              disabled={loading}
              required
            />
            <p className="text-xs text-muted-foreground">
              Full Docker image identifier including registry, repository, and tag
            </p>
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
              {loading ? "Adding..." : "Add Skill"}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
