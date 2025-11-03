"use client"

import { useState } from "react"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { adminApiClient, JobStatus } from "@/lib/admin-api"
import { Loader2, CheckCircle2, XCircle, AlertCircle } from "lucide-react"

interface ImportDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onImportComplete: () => void
}

export function ImportDialog({ open, onOpenChange, onImportComplete }: ImportDialogProps) {
  const [source, setSource] = useState("")
  const [headers, setHeaders] = useState("")
  const [updateExisting, setUpdateExisting] = useState(false)
  const [loading, setLoading] = useState(false)
  const [jobStatus, setJobStatus] = useState<JobStatus | null>(null)

  const handleImport = async () => {
    if (!source.trim()) {
      return
    }

    setLoading(true)
    setJobStatus(null)

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

      // Start the import job
      const response = await adminApiClient.importServers({
        source: source.trim(),
        headers: Object.keys(headerMap).length > 0 ? headerMap : undefined,
        update: updateExisting,
      })

      // Poll for job completion
      const finalJob = await adminApiClient.pollJobUntilComplete(
        response.job_id,
        (job) => {
          // Update UI with progress
          setJobStatus(job)
        },
        1000 // Poll every second
      )

      // Final update
      setJobStatus(finalJob)

      if (finalJob.status === 'completed') {
        // Wait a bit to show success message, then close and refresh
        setTimeout(() => {
          onOpenChange(false)
          onImportComplete()
          // Reset form
          setSource("")
          setHeaders("")
          setUpdateExisting(false)
          setJobStatus(null)
        }, 2000)
      }
    } catch (err) {
      // Create a failed job status for display
      setJobStatus({
        id: 'error',
        type: 'import',
        status: 'failed',
        progress: 0,
        error: err instanceof Error ? err.message : "Import failed",
        message: "Failed to start import",
        created_at: new Date().toISOString(),
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

          {jobStatus && (
            <div>
              {/* Progress bar for running jobs */}
              {jobStatus.status === 'running' && (
                <div className="mb-4">
                  <div className="flex items-center justify-between mb-2">
                    <span className="text-sm font-medium">Import Progress</span>
                    <span className="text-sm text-muted-foreground">{jobStatus.progress}%</span>
                  </div>
                  <div className="w-full bg-gray-200 rounded-full h-2.5 dark:bg-gray-700">
                    <div 
                      className="bg-blue-600 h-2.5 rounded-full transition-all duration-300"
                      style={{ width: `${jobStatus.progress}%` }}
                    ></div>
                  </div>
                  <p className="text-xs text-muted-foreground mt-2">{jobStatus.message}</p>
                </div>
              )}

              {/* Result display for completed/failed jobs */}
              {(jobStatus.status === 'completed' || jobStatus.status === 'failed') && (
                <div className={`p-4 rounded-lg border ${
                  jobStatus.status === 'completed'
                    ? 'bg-green-50 border-green-200 dark:bg-green-950 dark:border-green-800' 
                    : 'bg-red-50 border-red-200 dark:bg-red-950 dark:border-red-800'
                }`}>
                  <div className="flex items-start gap-3">
                    {jobStatus.status === 'completed' ? (
                      <CheckCircle2 className="h-5 w-5 text-green-600 dark:text-green-400 mt-0.5" />
                    ) : (
                      <XCircle className="h-5 w-5 text-red-600 dark:text-red-400 mt-0.5" />
                    )}
                    <div className="flex-1">
                      <p className={`font-medium ${
                        jobStatus.status === 'completed'
                          ? 'text-green-900 dark:text-green-100' 
                          : 'text-red-900 dark:text-red-100'
                      }`}>
                        {jobStatus.status === 'completed' ? jobStatus.result?.message : jobStatus.error}
                      </p>
                      {jobStatus.result && jobStatus.result.servers_created > 0 && (
                        <p className="text-sm mt-1 text-green-800 dark:text-green-200">
                          Successfully imported {jobStatus.result.servers_created} server{jobStatus.result.servers_created !== 1 ? 's' : ''}
                        </p>
                      )}
                      {jobStatus.result && jobStatus.result.servers_failed > 0 && (
                        <p className="text-sm mt-1 text-red-800 dark:text-red-200">
                          Failed to import {jobStatus.result.servers_failed} server{jobStatus.result.servers_failed !== 1 ? 's' : ''}
                        </p>
                      )}
                      {jobStatus.result?.failed_servers && jobStatus.result.failed_servers.length > 0 && (
                        <details className="mt-2">
                          <summary className="text-sm cursor-pointer text-red-800 dark:text-red-200">
                            View failed servers
                          </summary>
                          <ul className="mt-2 text-xs space-y-1 text-red-700 dark:text-red-300">
                            {jobStatus.result.failed_servers.map((server, i) => (
                              <li key={i}>{server}</li>
                            ))}
                          </ul>
                        </details>
                      )}
                    </div>
                  </div>
                </div>
              )}
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

