"use client"

import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

interface ImportSkillsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onImportComplete: () => void
}

export function ImportSkillsDialog({ open, onOpenChange }: ImportSkillsDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Import Skills</DialogTitle>
          <DialogDescription>
            Import skills from an external registry or seed file
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-4">
          <div className="space-y-2">
            <Label htmlFor="source">Source URL or File Path</Label>
            <Input
              id="source"
              placeholder="https://registry.example.com/v0/skills"
              disabled
            />
            <p className="text-xs text-muted-foreground">
              Feature coming soon - import skills from external registries
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

