"use client"

import { useState } from "react"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { adminApiClient } from "@/lib/admin-api"
import { Loader2, CheckCircle2, XCircle, AlertCircle } from "lucide-react"

interface ImportDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onImportComplete: () => void
}

interface ImportStatus {
  success: boolean
  message: string
}

export function ImportDialog({ open, onOpenChange, onImportComplete }: ImportDialogProps) {
  const [source, setSource] = useState("")
  const [headers, setHeaders] = useState("")
  const [updateExisting, setUpdateExisting] = useState(false)
  const [loading, setLoading] = useState(false)
  const [importStatus, setImportStatus] = useState<ImportStatus | null>(null)

  const handleImport = async () => {
    if (!source.trim()) {
      return
    }

    setLoading(true)
    setImportStatus(null)

    try {
      // Parse headers if provided
      const headerMap: Record<string, string> = {}
      if (headers.trim()) {
        const lines = headers.split('\n')
        for (const line of lines) {
          const [key, ...valueParts] = line.split(':')
          if (key && valueParts.length > 0) {
            headerMap[key.trim()] = valueParts.join(':').trim()
          }
        }
      }

      // Import servers
      const response = await adminApiClient.importServers({
        source: source.trim(),
        headers: Object.keys(headerMap).length > 0 ? headerMap : undefined,
        update: updateExisting,
      })

      setImportStatus({
        success: response.success,
        message: response.message,
      })

      if (response.success) {
        // Wait a bit to show success message, then close and refresh
        setTimeout(() => {
          onOpenChange(false)
          onImportComplete()
          // Reset form
          setSource("")
          setHeaders("")
          setUpdateExisting(false)
          setImportStatus(null)
        }, 2000)
      }
    } catch (err) {
      setImportStatus({
        success: false,
        message: err instanceof Error ? err.message : "Import failed",
      })
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[80vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Import Servers</DialogTitle>
          <DialogDescription>
            Import MCP servers from an external registry or seed file
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-4">
          <div className="space-y-2">
            <Label htmlFor="source">Source URL or File Path</Label>
            <Input
              id="source"
              placeholder="https://registry.example.com/v0/servers"
              value={source}
              onChange={(e) => setSource(e.target.value)}
              disabled={loading}
            />
            <p className="text-xs text-muted-foreground">
              Enter a registry API endpoint (ending with /v0/servers) or a direct URL to a JSON seed file
            </p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="headers">HTTP Headers (Optional)</Label>
            <Textarea
              id="headers"
              placeholder="Authorization: Bearer token&#10;X-Custom-Header: value"
              value={headers}
              onChange={(e) => setHeaders(e.target.value)}
              rows={3}
              disabled={loading}
            />
            <p className="text-xs text-muted-foreground">
              One header per line in format: Header-Name: value
            </p>
          </div>

          <div className="flex items-center space-x-2">
            <input
              type="checkbox"
              id="update"
              checked={updateExisting}
              onChange={(e) => setUpdateExisting(e.target.checked)}
              disabled={loading}
              className="h-4 w-4"
            />
            <Label htmlFor="update" className="cursor-pointer">
              Update existing servers if they already exist
            </Label>
          </div>

          {importStatus && (
            <div className={`p-4 rounded-lg border ${
              importStatus.success
                ? 'bg-green-50 border-green-200 dark:bg-green-950 dark:border-green-800' 
                : 'bg-red-50 border-red-200 dark:bg-red-950 dark:border-red-800'
            }`}>
              <div className="flex items-start gap-3">
                {importStatus.success ? (
                  <CheckCircle2 className="h-5 w-5 text-green-600 dark:text-green-400 mt-0.5" />
                ) : (
                  <XCircle className="h-5 w-5 text-red-600 dark:text-red-400 mt-0.5" />
                )}
                <div className="flex-1">
                  <p className={`font-medium ${
                    importStatus.success
                      ? 'text-green-900 dark:text-green-100' 
                      : 'text-red-900 dark:text-red-100'
                  }`}>
                    {importStatus.message}
                  </p>
                </div>
              </div>
            </div>
          )}

          <div className="flex items-center gap-3 p-3 rounded-lg bg-blue-50 border border-blue-200 dark:bg-blue-950 dark:border-blue-800">
            <AlertCircle className="h-5 w-5 text-blue-600 dark:text-blue-400" />
            <div className="text-sm text-blue-900 dark:text-blue-100">
              <p className="font-medium">Common Registry URLs:</p>
              <ul className="mt-1 space-y-1 text-xs">
                <li>• Official MCP Registry: <code>https://registry.modelcontextprotocol.io/v0.1/servers</code></li>
                <li>• Your own registry: <code>https://your-registry.com/v0/servers</code></li>
              </ul>
            </div>
          </div>
        </div>

        <div className="flex justify-end gap-2">
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={loading}
          >
            Cancel
          </Button>
          <Button
            onClick={handleImport}
            disabled={loading || !source.trim()}
          >
            {loading && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            Import
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}

