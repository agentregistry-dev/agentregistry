"use client"

import { useEffect, useState } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { AlertCircle, ExternalLink } from "lucide-react"
import { client } from "@/lib/admin-api"
import { listRemotemcpservers } from "@/lib/api/sdk.gen"
import type { RemoteMcpServer } from "@/lib/api/types.gen"

const RELATED_ANNOTATION = "agentregistry.dev/related-mcpserver"

// Listing page for RemoteMCPServer, the register-only MCP endpoint kind.
// It surfaces metadata + endpoint URL + the optional `related-mcpserver`
// annotation that links a remote row back to a bundled sibling. Detail editing
// lands in a follow-up; this view is catalog-only.
export default function RemoteResourcesPage() {
  const [servers, setServers] = useState<RemoteMcpServer[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false

    async function load() {
      try {
        setLoading(true)
        setError(null)

        const serverRes = await listRemotemcpservers({ client, throwOnError: true, query: { namespace: "all", limit: 100 } })
        if (cancelled) return
        setServers(serverRes.data?.items ?? [])
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
          Already-running MCP servers the registry calls without managing their lifecycle.
        </p>
      </header>

      {error && (
        <div className="mb-4 flex items-center gap-2 rounded-md border border-destructive bg-destructive/10 px-4 py-3 text-sm">
          <AlertCircle className="h-4 w-4" />
          <span>{error}</span>
        </div>
      )}

      <div className="mb-4">
        <Badge variant="secondary">MCP servers {servers.length}</Badge>
      </div>

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
