"use client"

import { useState } from "react"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { createSkillV0, type SkillJson } from "@/lib/admin-api"

interface AddSkillDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSkillAdded: () => void
}

export function AddSkillDialog({ open, onOpenChange, onSkillAdded }: AddSkillDialogProps) {
  const [name, setName] = useState("")
  const [description, setDescription] = useState("")
  const [version, setVersion] = useState("latest")
  const [publishSource, setPublishSource] = useState<"git" | "docker">("git")
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
      const trimmedRepositoryUrl = repositoryUrl.trim()
      const trimmedDockerImage = dockerImage.trim()
      if (publishSource === "git" && !trimmedRepositoryUrl) {
        throw new Error("Repository URL is required")
      }
      if (publishSource === "docker" && !trimmedDockerImage) {
        throw new Error("Docker image is required for Docker publish")
      }

      // Construct the SkillJSON object
      const skillData: SkillJson = {
        name: name.trim(),
        description: description.trim(),
        version: version.trim(),
        repository: publishSource === "git" ? {
          url: trimmedRepositoryUrl,
          source: "git"
        } : undefined,
        packages: publishSource === "docker" ? [
          {
            registryType: "docker",
            identifier: trimmedDockerImage,
            version: version.trim(),
            transport: {
              type: "docker"
            }
          }
        ] : undefined
      }

      // Create the skill
      await createSkillV0({ body: skillData, throwOnError: true })

      // Reset form
      setName("")
      setDescription("")
      setVersion("latest")
      setPublishSource("git")
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
    setPublishSource("git")
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
            <Label>
              Publish Source <span className="text-red-500">*</span>
            </Label>
            <div className="flex gap-2">
              <Button
                type="button"
                variant={publishSource === "git" ? "default" : "outline"}
                onClick={() => setPublishSource("git")}
                disabled={loading}
              >
                Git Repository
              </Button>
              <Button
                type="button"
                variant={publishSource === "docker" ? "default" : "outline"}
                onClick={() => setPublishSource("docker")}
                disabled={loading}
              >
                Docker Image
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              Select one publish source mode for this skill version
            </p>
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

          {publishSource === "git" ? (
            <div className="space-y-2">
              <Label htmlFor="repositoryUrl">
                Repository URL <span className="text-red-500">*</span>
              </Label>
              <Input
                id="repositoryUrl"
                placeholder="https://github.com/username/repo"
                value={repositoryUrl}
                onChange={(e) => setRepositoryUrl(e.target.value)}
                disabled={loading}
                type="url"
                required
              />
              <p className="text-xs text-muted-foreground">
                Link to the skill&apos;s Git repository (GitHub, GitLab, Bitbucket, etc.)
              </p>
            </div>
          ) : (
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
                Full image reference including registry, repository, and tag
              </p>
            </div>
          )}

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
