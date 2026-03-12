"use client"

import { useState } from "react"
import { SkillResponse } from "@/lib/admin-api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Package,
  Calendar,
  ExternalLink,
  Globe,
  Code,
  Link,
  Zap,
  Copy,
  Check,
} from "lucide-react"

interface SkillDetailProps {
  skill: SkillResponse
}

export function SkillDetail({ skill }: SkillDetailProps) {
  const [activeTab, setActiveTab] = useState("overview")
  const [jsonCopied, setJsonCopied] = useState(false)

  const { skill: skillData, _meta } = skill
  const official = _meta?.['io.modelcontextprotocol.registry/official']

  const handleCopyJson = async () => {
    try {
      await navigator.clipboard.writeText(JSON.stringify(skill, null, 2))
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
            <Zap className="h-6 w-6 text-primary" />
          </div>
          <div className="flex-1 min-w-0">
            <h1 className="text-2xl font-bold truncate mb-1">{skillData.title || skillData.name}</h1>
            <p className="text-[15px] text-muted-foreground">{skillData.name}</p>
          </div>
        </div>

        {/* Quick info */}
        <div className="flex flex-wrap gap-2">
          <span className="flex items-center gap-1.5 px-2.5 py-1 bg-muted rounded text-sm font-mono">
            {skillData.version}
          </span>
          {official?.publishedAt && (
            <span className="flex items-center gap-1.5 px-2.5 py-1 bg-muted rounded text-sm">
              <Calendar className="h-3 w-3 text-muted-foreground" />
              {formatDate(official.publishedAt)}
            </span>
          )}
          {skillData.websiteUrl && (
            <a
              href={skillData.websiteUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-center gap-1.5 px-2.5 py-1 bg-muted rounded text-sm hover:bg-muted/80 transition-colors text-primary"
            >
              <Globe className="h-3 w-3" />
              Website
              <ExternalLink className="h-2.5 w-2.5" />
            </a>
          )}
        </div>

        <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full">
          <TabsList className="mb-4">
            <TabsTrigger value="overview">Overview</TabsTrigger>
            {skillData.packages && skillData.packages.length > 0 && (
              <TabsTrigger value="packages">Packages</TabsTrigger>
            )}
            {skillData.remotes && skillData.remotes.length > 0 && (
              <TabsTrigger value="remotes">Remotes</TabsTrigger>
            )}
            <TabsTrigger value="raw">Raw</TabsTrigger>
          </TabsList>

          <TabsContent value="overview" className="space-y-6">
            <section>
              <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-2">Description</h3>
              <p className="text-[15px] leading-relaxed">{skillData.description}</p>
            </section>

            {skillData.repository?.url && (
              <section>
                <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-2">Repository</h3>
                <div className="space-y-1.5 text-sm">
                  {skillData.repository.source && (
                    <div className="flex items-center justify-between text-xs">
                      <span className="text-muted-foreground">Source</span>
                      <Badge variant="outline" className="text-[10px]">{skillData.repository.source}</Badge>
                    </div>
                  )}
                  <div className="flex items-center justify-between text-xs">
                    <span className="text-muted-foreground">URL</span>
                    <a
                      href={skillData.repository.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-primary hover:underline flex items-center gap-1"
                    >
                      {skillData.repository.url} <ExternalLink className="h-2.5 w-2.5" />
                    </a>
                  </div>
                </div>
              </section>
            )}
          </TabsContent>

          <TabsContent value="packages" className="space-y-3">
            {skillData.packages && skillData.packages.length > 0 ? (
              skillData.packages.map((pkg, i) => (
                <div key={i} className="p-4 rounded-lg border">
                  <div className="flex items-center justify-between mb-2">
                    <div className="flex items-center gap-2">
                      <Package className="h-4 w-4 text-primary" />
                      <h4 className="text-sm font-semibold">{pkg.identifier}</h4>
                    </div>
                    <Badge variant="outline" className="text-xs">{pkg.registryType}</Badge>
                  </div>
                  <div className="space-y-1 text-xs">
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">Version</span>
                      <span className="font-mono">{pkg.version}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">Transport</span>
                      <span className="font-mono">{pkg.transport?.type || 'N/A'}</span>
                    </div>
                  </div>
                </div>
              ))
            ) : (
              <p className="text-center text-sm text-muted-foreground py-8">No packages defined</p>
            )}
          </TabsContent>

          <TabsContent value="remotes" className="space-y-3">
            {skillData.remotes && skillData.remotes.length > 0 ? (
              skillData.remotes.map((remote, i) => (
                <div key={i} className="p-4 rounded-lg border">
                  <div className="flex items-center justify-between mb-2">
                    <div className="flex items-center gap-2">
                      <ExternalLink className="h-4 w-4 text-primary" />
                      <h4 className="text-sm font-semibold">Remote {i + 1}</h4>
                    </div>
                  </div>
                  {remote.url && (
                    <div className="flex items-center gap-1.5 text-xs">
                      <Link className="h-3 w-3 text-muted-foreground" />
                      <a
                        href={remote.url}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="text-primary hover:underline break-all"
                      >
                        {remote.url}
                      </a>
                    </div>
                  )}
                </div>
              ))
            ) : (
              <p className="text-center text-sm text-muted-foreground py-8">No remotes defined</p>
            )}
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
                {JSON.stringify(skill, null, 2)}
              </pre>
            </div>
          </TabsContent>
        </Tabs>
    </div>
  )
}
