"use client"

import { useState } from "react"
import type { Plugin } from "@/lib/api/types.gen"
import { getPinnedRevision, getPluginRepoUrl, getPluginResolution } from "@/lib/plugin-utils"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import {
  AlertCircle,
  AlertTriangle,
  Blocks,
  Bot,
  Calendar,
  Check,
  CheckCircle2,
  Code,
  Copy,
  ExternalLink,
  GitCommitHorizontal,
  History,
  Loader2,
  Terminal,
  Webhook,
  Zap,
} from "lucide-react"
import MCPIcon from "@/components/icons/mcp"

interface PluginDetailProps {
  plugin: Plugin
  allTags?: Plugin[]
}

function ResolutionSummary({ plugin }: { plugin: Plugin }) {
  const resolution = getPluginResolution(plugin)

  switch (resolution.state) {
    case "ready":
      return (
        <span className="flex items-center gap-1.5 px-2.5 py-1 bg-muted rounded text-sm text-green-700 dark:text-green-400">
          <CheckCircle2 className="h-3 w-3" /> Ready
        </span>
      )
    case "progressing":
      return (
        <span className="flex items-center gap-1.5 px-2.5 py-1 bg-muted rounded text-sm text-amber-700 dark:text-amber-400">
          <Loader2 className="h-3 w-3 animate-spin" /> Resolving source…
        </span>
      )
    case "failed":
      return (
        <span className="flex items-center gap-1.5 px-2.5 py-1 bg-destructive/10 rounded text-sm text-destructive">
          <AlertCircle className="h-3 w-3" /> {resolution.label}
        </span>
      )
    default:
      return (
        <span className="flex items-center gap-1.5 px-2.5 py-1 bg-muted rounded text-sm text-muted-foreground">
          Not resolved
        </span>
      )
  }
}

function SectionHeading({ children }: { children: React.ReactNode }) {
  return (
    <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-2">
      {children}
    </h3>
  )
}

function InfoRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-4 text-xs">
      <span className="text-muted-foreground shrink-0">{label}</span>
      <span className="min-w-0 text-right break-all">{children}</span>
    </div>
  )
}

export function PluginDetail({ plugin, allTags: allTagsProp }: PluginDetailProps) {
  const [activeTab, setActiveTab] = useState("overview")
  const [jsonCopied, setJsonCopied] = useState(false)
  const [selectedTag, setSelectedTag] = useState<Plugin>(plugin)

  const allTags = allTagsProp || [plugin]

  const { metadata, spec, status } = selectedTag
  const resolution = getPluginResolution(selectedTag)
  const pinnedRevision = getPinnedRevision(selectedTag)
  const repoUrl = getPluginRepoUrl(selectedTag)
  const gitRepository = spec.source?.git?.repository
  const manifest = status?.manifest
  const inventory = status?.inventory
  const hooks = inventory?.hooks ?? []
  const executables = inventory?.executables ?? []
  const skills = inventory?.skills ?? []
  const commands = inventory?.commands ?? []
  const agents = inventory?.agents ?? []
  const mcpServers = inventory?.mcpServers ?? []
  const hasInventory =
    hooks.length > 0 || executables.length > 0 || skills.length > 0 ||
    commands.length > 0 || agents.length > 0 || mcpServers.length > 0

  const handleTagChange = (tag: string) => {
    const newTag = allTags.find((v) => v.metadata.tag === tag)
    if (newTag) setSelectedTag(newTag)
  }

  const handleCopyJson = async () => {
    try {
      await navigator.clipboard.writeText(JSON.stringify(selectedTag, null, 2))
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
          <Blocks className="h-6 w-6 text-primary" />
        </div>
        <div className="flex-1 min-w-0">
          <h1 className="text-2xl font-bold truncate mb-1">{spec.title || metadata.name}</h1>
          <p className="text-[15px] text-muted-foreground">{metadata.name}</p>
        </div>
      </div>

      {/* Tag selector */}
      {allTags.length > 1 && (
        <div className="flex items-center gap-3 px-3 py-2 bg-accent/50 border border-primary/10 rounded-md">
          <History className="h-4 w-4 text-muted-foreground" />
          <span className="text-sm">{allTags.length} tags</span>
          <Select value={selectedTag.metadata.tag} onValueChange={handleTagChange}>
            <SelectTrigger className="w-[160px] h-7 text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {allTags.map((tag) => (
                <SelectItem key={tag.metadata.tag} value={tag.metadata.tag ?? ""}>
                  {tag.metadata.tag}
                  {tag.metadata.tag === plugin.metadata.tag && " (latest)"}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      )}

      {/* Quick info */}
      <div className="flex flex-wrap gap-2">
        <ResolutionSummary plugin={selectedTag} />
        {metadata.tag && (
          <span className="flex items-center gap-1.5 px-2.5 py-1 bg-muted rounded text-sm">
            <span className="font-mono">{metadata.tag}</span>
            {allTags.length > 1 && (
              <Badge variant="secondary" className="text-[10px] px-1 py-0 h-3.5">{allTags.length} total</Badge>
            )}
          </span>
        )}
        {pinnedRevision && (
          <span className="flex items-center gap-1.5 px-2.5 py-1 bg-muted rounded text-sm font-mono">
            <GitCommitHorizontal className="h-3 w-3 text-muted-foreground" />
            {pinnedRevision}
          </span>
        )}
        {metadata.createdAt && (
          <span className="flex items-center gap-1.5 px-2.5 py-1 bg-muted rounded text-sm">
            <Calendar className="h-3 w-3 text-muted-foreground" />
            {formatDate(metadata.createdAt)}
          </span>
        )}
      </div>

      {/* Resolution failure detail */}
      {resolution.state === "failed" && resolution.message && (
        <div className="rounded-md border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">
          {resolution.message}
        </div>
      )}

      <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full">
        <TabsList className="mb-4">
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="contents">Contents</TabsTrigger>
          <TabsTrigger value="raw">Raw</TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="space-y-6">
          {spec.description && (
            <section>
              <SectionHeading>Description</SectionHeading>
              <p className="text-[15px] leading-relaxed">{spec.description}</p>
            </section>
          )}

          {spec.harnesses && spec.harnesses.length > 0 && (
            <section>
              <SectionHeading>Harnesses</SectionHeading>
              <div className="flex flex-wrap gap-1.5">
                {spec.harnesses.map((harness) => (
                  <Badge key={harness} variant="secondary" className="font-normal">{harness}</Badge>
                ))}
              </div>
            </section>
          )}

          <section>
            <SectionHeading>Source</SectionHeading>
            <div className="space-y-1.5 text-sm">
              {spec.source?.type && <InfoRow label="Type">{spec.source.type}</InfoRow>}
              {repoUrl && (
                <InfoRow label="Repository">
                  <a
                    href={repoUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-primary hover:underline inline-flex items-center gap-1"
                  >
                    {repoUrl} <ExternalLink className="h-2.5 w-2.5" />
                  </a>
                </InfoRow>
              )}
              {gitRepository?.branch && <InfoRow label="Branch">{gitRepository.branch}</InfoRow>}
              {gitRepository?.commit && <InfoRow label="Commit"><span className="font-mono">{gitRepository.commit}</span></InfoRow>}
              {gitRepository?.subfolder && <InfoRow label="Subfolder"><span className="font-mono">{gitRepository.subfolder}</span></InfoRow>}
              {spec.source?.oci?.reference && <InfoRow label="OCI Reference"><span className="font-mono">{spec.source.oci.reference}</span></InfoRow>}
              {status?.resolvedSource?.commit && (
                <InfoRow label="Pinned Commit"><span className="font-mono">{status.resolvedSource.commit}</span></InfoRow>
              )}
              {status?.resolvedSource?.digest && (
                <InfoRow label="Pinned Digest"><span className="font-mono">{status.resolvedSource.digest}</span></InfoRow>
              )}
            </div>
            {!status?.resolvedSource && resolution.state !== "failed" && (
              <p className="text-xs text-muted-foreground mt-2">
                The controller resolves this source to a pinned commit and scans its
                manifest and inventory. Refresh to see resolution progress.
              </p>
            )}
          </section>

          {manifest && (
            <section>
              <SectionHeading>Manifest</SectionHeading>
              <div className="space-y-1.5 text-sm">
                <InfoRow label="Name"><span className="font-mono">{manifest.name}</span></InfoRow>
                {manifest.displayName && <InfoRow label="Display Name">{manifest.displayName}</InfoRow>}
                {manifest.version && <InfoRow label="Version"><span className="font-mono">{manifest.version}</span></InfoRow>}
                {manifest.description && <InfoRow label="Description">{manifest.description}</InfoRow>}
                {manifest.author?.name && (
                  <InfoRow label="Author">
                    {manifest.author.name}
                    {manifest.author.email && <span className="text-muted-foreground"> &lt;{manifest.author.email}&gt;</span>}
                  </InfoRow>
                )}
                {manifest.license && <InfoRow label="License">{manifest.license}</InfoRow>}
                {manifest.homepage && (
                  <InfoRow label="Homepage">
                    <a
                      href={manifest.homepage}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-primary hover:underline inline-flex items-center gap-1"
                    >
                      {manifest.homepage} <ExternalLink className="h-2.5 w-2.5" />
                    </a>
                  </InfoRow>
                )}
              </div>
              {manifest.keywords && manifest.keywords.length > 0 && (
                <div className="flex flex-wrap gap-1.5 mt-2">
                  {manifest.keywords.map((keyword) => (
                    <Badge key={keyword} variant="outline" className="text-xs font-normal">{keyword}</Badge>
                  ))}
                </div>
              )}
            </section>
          )}
        </TabsContent>

        <TabsContent value="contents" className="space-y-6">
          {/* Governance risk surface: hooks and executables run arbitrary code
              on the consumer's machine, so they lead the inventory. */}
          {(hooks.length > 0 || executables.length > 0) && (
            <div className="rounded-md border border-amber-500/40 bg-amber-500/10 p-4 space-y-4">
              <div className="flex items-center gap-2 text-sm font-semibold text-amber-700 dark:text-amber-400">
                <AlertTriangle className="h-4 w-4" />
                Runs code on install
              </div>
              <p className="text-xs text-muted-foreground">
                This plugin registers lifecycle hooks and/or ships executables. Review
                them before deploying — they execute with the harness&apos;s permissions.
              </p>
              {hooks.length > 0 && (
                <div>
                  <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-1.5 flex items-center gap-1.5">
                    <Webhook className="h-3.5 w-3.5" /> Hooks ({hooks.length})
                  </h4>
                  <ul className="space-y-1">
                    {hooks.map((hook, i) => (
                      <li key={`${hook.event}-${i}`} className="text-sm flex items-center gap-2">
                        <span className="font-mono">{hook.event}</span>
                        {hook.type && <Badge variant="outline" className="text-[10px] px-1 py-0 h-4 font-normal">{hook.type}</Badge>}
                      </li>
                    ))}
                  </ul>
                </div>
              )}
              {executables.length > 0 && (
                <div>
                  <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-1.5 flex items-center gap-1.5">
                    <Terminal className="h-3.5 w-3.5" /> Executables ({executables.length})
                  </h4>
                  <ul className="space-y-1">
                    {executables.map((exe) => (
                      <li key={exe} className="text-sm font-mono">{exe}</li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          )}

          {skills.length > 0 && (
            <section>
              <SectionHeading>
                <span className="inline-flex items-center gap-1.5"><Zap className="h-3.5 w-3.5" /> Skills ({skills.length})</span>
              </SectionHeading>
              <ul className="space-y-2">
                {skills.map((skill) => (
                  <li key={skill.name} className="text-sm">
                    <span className="font-mono font-medium">{skill.name}</span>
                    {skill.description && (
                      <p className="text-muted-foreground text-xs mt-0.5">{skill.description}</p>
                    )}
                  </li>
                ))}
              </ul>
            </section>
          )}

          {commands.length > 0 && (
            <section>
              <SectionHeading>
                <span className="inline-flex items-center gap-1.5"><Terminal className="h-3.5 w-3.5" /> Commands ({commands.length})</span>
              </SectionHeading>
              <div className="flex flex-wrap gap-1.5">
                {commands.map((command) => (
                  <Badge key={command} variant="secondary" className="font-mono font-normal">/{command}</Badge>
                ))}
              </div>
            </section>
          )}

          {agents.length > 0 && (
            <section>
              <SectionHeading>
                <span className="inline-flex items-center gap-1.5"><Bot className="h-3.5 w-3.5" /> Agents ({agents.length})</span>
              </SectionHeading>
              <div className="flex flex-wrap gap-1.5">
                {agents.map((agent) => (
                  <Badge key={agent} variant="secondary" className="font-mono font-normal">{agent}</Badge>
                ))}
              </div>
            </section>
          )}

          {mcpServers.length > 0 && (
            <section>
              <SectionHeading>
                <span className="inline-flex items-center gap-1.5">
                  <span className="h-3.5 w-3.5 flex items-center justify-center"><MCPIcon /></span> MCP Servers ({mcpServers.length})
                </span>
              </SectionHeading>
              <div className="flex flex-wrap gap-1.5">
                {mcpServers.map((server) => (
                  <Badge key={server} variant="secondary" className="font-mono font-normal">{server}</Badge>
                ))}
              </div>
            </section>
          )}

          {!hasInventory && (
            <p className="text-sm text-muted-foreground">
              {resolution.state === "ready"
                ? "The controller found no skills, commands, agents, hooks, MCP servers, or executables in this bundle."
                : "Inventory is populated by the controller once the source is resolved and scanned."}
            </p>
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
              {JSON.stringify(selectedTag, null, 2)}
            </pre>
          </div>
        </TabsContent>
      </Tabs>
    </div>
  )
}
