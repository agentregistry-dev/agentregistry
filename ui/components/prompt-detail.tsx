"use client"

import { useState } from "react"
import { PromptResponse } from "@/lib/admin-api"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import {
  Calendar,
  FileText,
  Code,
  Clock,
  Copy,
  Check,
  History,
} from "lucide-react"

interface PromptDetailProps {
  prompt: PromptResponse
  allVersions?: PromptResponse[]
}

export function PromptDetail({ prompt, allVersions: allVersionsProp }: PromptDetailProps) {
  const [activeTab, setActiveTab] = useState("overview")
  const [jsonCopied, setJsonCopied] = useState(false)
  const [selectedVersion, setSelectedVersion] = useState<PromptResponse>(prompt)

  const allVersions = allVersionsProp || [prompt]

  const { prompt: promptData, _meta } = selectedVersion
  const official = _meta?.['io.modelcontextprotocol.registry/official']

  const handleVersionChange = (version: string) => {
    const newVersion = allVersions.find(v => v.prompt.version === version)
    if (newVersion) setSelectedVersion(newVersion)
  }

  const handleCopyJson = async () => {
    try {
      await navigator.clipboard.writeText(JSON.stringify(selectedVersion, null, 2))
      setJsonCopied(true)
      setTimeout(() => setJsonCopied(false), 2000)
    } catch (err) {
      console.error('Failed to copy JSON:', err)
    }
  }

  const formatDate = (dateString: string) => {
    try {
      return new Date(dateString).toLocaleString('en-US', {
        year: 'numeric',
        month: 'long',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
      })
    } catch {
      return dateString
    }
  }

  return (
    <div className="space-y-6">
        {/* Header */}
        <div className="flex items-start gap-4">
          <div className="w-12 h-12 rounded bg-primary/8 flex items-center justify-center flex-shrink-0">
            <FileText className="h-6 w-6 text-primary" />
          </div>
          <div className="flex-1 min-w-0">
            <h1 className="text-2xl font-bold truncate mb-1">{promptData.name}</h1>
            {promptData.description && (
              <p className="text-[15px] text-muted-foreground">{promptData.description}</p>
            )}
          </div>
        </div>

        {/* Version selector */}
        {allVersions.length > 1 && (
          <div className="flex items-center gap-3 px-3 py-2 bg-accent/50 border border-primary/10 rounded-md">
            <History className="h-4 w-4 text-muted-foreground" />
            <span className="text-sm">{allVersions.length} versions</span>
            <Select value={selectedVersion.prompt.version} onValueChange={handleVersionChange}>
              <SelectTrigger className="w-[160px] h-7 text-sm">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {allVersions.map((version) => (
                  <SelectItem key={version.prompt.version} value={version.prompt.version}>
                    {version.prompt.version}
                    {version.prompt.version === prompt.prompt.version && " (latest)"}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}

        {/* Quick info */}
        <div className="flex flex-wrap gap-2">
          <span className="flex items-center gap-1.5 px-2.5 py-1 bg-muted rounded text-sm">
            <span className="font-mono">{promptData.version}</span>
            {allVersions.length > 1 && (
              <Badge variant="secondary" className="text-[10px] px-1 py-0 h-3.5">{allVersions.length} total</Badge>
            )}
          </span>
          {official?.publishedAt && (
            <span className="flex items-center gap-1.5 px-2.5 py-1 bg-muted rounded text-sm">
              <Calendar className="h-3 w-3 text-muted-foreground" />
              {formatDate(official.publishedAt)}
            </span>
          )}
          {official?.updatedAt && (
            <span className="flex items-center gap-1.5 px-2.5 py-1 bg-muted rounded text-sm">
              <Clock className="h-3 w-3 text-muted-foreground" />
              {formatDate(official.updatedAt)}
            </span>
          )}
        </div>

        <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full">
          <TabsList className="mb-4">
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="raw">Raw</TabsTrigger>
          </TabsList>

          <TabsContent value="overview" className="space-y-6">
            {promptData.description && (
              <section>
                <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-2">Description</h3>
                <p className="text-[15px] leading-relaxed">{promptData.description}</p>
              </section>
            )}

            <section>
              <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-2">Content</h3>
              <pre className="bg-muted p-4 rounded-md overflow-x-auto text-sm whitespace-pre-wrap break-words leading-relaxed">
                {promptData.content}
              </pre>
            </section>
          </TabsContent>

          <TabsContent value="raw">
            <div className="rounded-lg border p-4">
              <div className="flex items-center justify-between mb-3">
                <h3 className="text-sm font-semibold flex items-center gap-2">
                  <Code className="h-4 w-4" />
                  Raw JSON
                </h3>
                <Button variant="outline" size="sm" onClick={handleCopyJson} className="gap-1.5 h-7 text-xs">
                  {jsonCopied ? <><Check className="h-3 w-3" /> Copied</> : <><Copy className="h-3 w-3" /> Copy</>}
                </Button>
              </div>
              <pre className="bg-muted p-3 rounded-md overflow-x-auto text-xs leading-relaxed">
                {JSON.stringify(selectedVersion, null, 2)}
              </pre>
            </div>
          </TabsContent>
        </Tabs>
    </div>
  )
}
