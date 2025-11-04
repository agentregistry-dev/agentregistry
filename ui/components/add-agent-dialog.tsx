"use client"

import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"

interface AddAgentDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onAgentAdded: () => void
}

export function AddAgentDialog({ open, onOpenChange }: AddAgentDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[80vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Add Agent</DialogTitle>
          <DialogDescription>
            Add a new agent to the registry
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-4">
          <div className="space-y-2">
            <Label htmlFor="name">Agent Name</Label>
            <Input
              id="name"
              placeholder="my-agent"
              disabled
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="title">Title</Label>
            <Input
              id="title"
              placeholder="My Agent"
              disabled
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="description">Description</Label>
            <Textarea
              id="description"
              placeholder="A description of what this agent does"
              rows={3}
              disabled
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="version">Version</Label>
            <Input
              id="version"
              placeholder="1.0.0"
              disabled
            />
          </div>

          <p className="text-xs text-muted-foreground">
            Feature coming soon - manually add agents to the registry
          </p>

          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button disabled>
              Add Agent
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

