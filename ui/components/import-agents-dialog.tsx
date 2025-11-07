"use client"

import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

interface ImportAgentsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onImportComplete: () => void
}

export function ImportAgentsDialog({ open, onOpenChange }: ImportAgentsDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Import Agents</DialogTitle>
          <DialogDescription>
            Import agents from an external registry or seed file
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-4">
          <div className="space-y-2">
            <Label htmlFor="source">Source URL or File Path</Label>
            <Input
              id="source"
              placeholder="https://registry.example.com/v0/agents"
              disabled
            />
            <p className="text-xs text-muted-foreground">
              Feature coming soon - import agents from external registries
            </p>
          </div>

          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button disabled>
              Import
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

