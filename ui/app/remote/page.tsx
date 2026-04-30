"use client"

import { useEffect, useState } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { AlertCircle, ExternalLink } from "lucide-react"
import { client } from "@/lib/admin-api"
import { listRemotemcpservers, listRemoteagents } from "@/lib/api/sdk.gen"
import type { RemoteAgent, RemoteMcpServer } from "@/lib/api/types.gen"

const RELATED_ANNOTATION = "agentregistry.dev/related-mcpserver"

// Listing pages for the two register-only kinds: RemoteMCPServer and
// RemoteAgent. Both surface metadata + endpoint URL + the optional
// `related-mcpserver` annotation that links a remote row back to a
// bundled sibling. Detail editing lands in a follow-up; this view is
// catalog-only.
export default function RemoteResourcesPage() {
  const [tab, setTab] = useState<"servers" | "agents">("servers")
  const [servers, setServers] = useState<RemoteMcpServer[]>([])
  const [agents, setAgents] = useState<RemoteAgent[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false

    async function load() {
      try {
        setLoading(true)
        setError(null)

        const [serverRes, agentRes] = await Promise.all([
          listRemotemcpservers({ client, throwOnError: true, query: { namespace: "all", limit: 100 } }),
          listRemoteagents({ client, throwOnError: true, query: { namespace: "all", limit: 100 } }),
        ])
        if (cancelled) return
        setServers(serverRes.data?.items ?? [])
        setAgents(agentRes.data?.items ?? [])
      } catch (err: unknown) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : "Failed to load remote resources")
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    }

    void load()
    return () => {
      cancelled = true
    }
  }, [])

  return (
    <div className="container mx-auto px-6 py-8">
      <header className="mb-6">
        <h1 className="text-2xl font-semibold">Remote resources</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          Already-running MCP servers and agents the registry calls without managing their lifecycle.
        </p>
      </header>

      {error && (
        <div className="mb-4 flex items-center gap-2 rounded-md border border-destructive bg-destructive/10 px-4 py-3 text-sm">
          <AlertCircle className="h-4 w-4" />
          <span>{error}</span>
        </div>
      )}

      <Tabs value={tab} onValueChange={(v) => setTab(v as "servers" | "agents")} className="w-full">
        <TabsList>
          <TabsTrigger value="servers">
            MCP servers <Badge variant="secondary" className="ml-2">{servers.length}</Badge>
          </TabsTrigger>
          <TabsTrigger value="agents">
            Agents <Badge variant="secondary" className="ml-2">{agents.length}</Badge>
          </TabsTrigger>
        </TabsList>

        <TabsContent value="servers" className="mt-4">
          {loading ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : servers.length === 0 ? (
            <EmptyState>No RemoteMCPServer resources yet.</EmptyState>
          ) : (
            <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
              {servers.map((s) => (
                <RemoteServerCard key={cardKey(s.metadata)} server={s} />
              ))}
            </div>
          )}
        </TabsContent>

        <TabsContent value="agents" className="mt-4">
          {loading ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : agents.length === 0 ? (
            <EmptyState>No RemoteAgent resources yet.</EmptyState>
          ) : (
            <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
              {agents.map((a) => (
                <RemoteAgentCard key={cardKey(a.metadata)} agent={a} />
              ))}
            </div>
          )}
        </TabsContent>
      </Tabs>
    </div>
  )
}

function EmptyState({ children }: { children: React.ReactNode }) {
  return (
    <div className="rounded-md border border-dashed py-10 text-center text-sm text-muted-foreground">
      {children}
    </div>
  )
}

function RemoteServerCard({ server }: { server: RemoteMcpServer }) {
  const remote = server.spec.remote
  const related = server.metadata.annotations?.[RELATED_ANNOTATION]
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">
          {server.spec.title || server.metadata.name}
          <Badge variant="outline" className="ml-2 align-middle text-xs">
            v{server.metadata.version || "—"}
          </Badge>
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-2 text-sm">
        {server.spec.description && (
          <p className="text-muted-foreground">{server.spec.description}</p>
        )}
        <RemoteRow type={remote?.type} url={remote?.url} />
        {related && (
          <p className="text-muted-foreground text-xs">
            Sibling of bundled <code>MCPServer/{related}</code>.
          </p>
        )}
      </CardContent>
    </Card>
  )
}

function RemoteAgentCard({ agent }: { agent: RemoteAgent }) {
  const remote = agent.spec.remote
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">
          {agent.spec.title || agent.metadata.name}
          <Badge variant="outline" className="ml-2 align-middle text-xs">
            v{agent.metadata.version || "—"}
          </Badge>
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-2 text-sm">
        {agent.spec.description && (
          <p className="text-muted-foreground">{agent.spec.description}</p>
        )}
        <RemoteRow type={remote?.type} url={remote?.url} />
      </CardContent>
    </Card>
  )
}

function RemoteRow({ type, url }: { type?: string; url?: string }) {
  if (!url) return null
  return (
    <div className="flex items-center gap-2">
      {type && <Badge variant="secondary">{type}</Badge>}
      <a
        href={url}
        target="_blank"
        rel="noreferrer"
        className="inline-flex items-center gap-1 truncate text-primary hover:underline"
      >
        {url}
        <ExternalLink className="h-3.5 w-3.5" />
      </a>
    </div>
  )
}

function cardKey(meta: { namespace?: string; name: string; version?: string }): string {
  return `${meta.namespace ?? "default"}/${meta.name}@${meta.version ?? ""}`
}
