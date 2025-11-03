"use client"

import { useEffect, useState } from "react"
import { Card } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { ServerCard } from "@/components/server-card"
import { ServerDetail } from "@/components/server-detail"
import { ImportDialog } from "@/components/import-dialog"
import { AddServerDialog } from "@/components/add-server-dialog"
import { adminApiClient, ServerResponse, ServerStats } from "@/lib/admin-api"
import {
  Server,
  Database,
  Search,
  Download,
  RefreshCw,
  CheckCircle,
  XCircle,
  Archive,
  Plus,
} from "lucide-react"

export default function AdminPage() {
  const [servers, setServers] = useState<ServerResponse[]>([])
  const [filteredServers, setFilteredServers] = useState<ServerResponse[]>([])
  const [stats, setStats] = useState<ServerStats | null>(null)
  const [searchQuery, setSearchQuery] = useState("")
  const [selectedServer, setSelectedServer] = useState<ServerResponse | null>(null)
  const [importDialogOpen, setImportDialogOpen] = useState(false)
  const [addServerDialogOpen, setAddServerDialogOpen] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [statusFilter, setStatusFilter] = useState<string>("all")

  // Fetch data from API
  const fetchData = async () => {
    try {
      setLoading(true)
      setError(null)
      
      // Fetch all servers (with pagination if needed)
      const allServers: ServerResponse[] = []
      let cursor: string | undefined
      
      do {
        const response = await adminApiClient.listServers({ 
          cursor, 
          limit: 100,
        })
        allServers.push(...response.servers)
        cursor = response.metadata.nextCursor
      } while (cursor)
      
      setServers(allServers)
      
      // Fetch stats
      const statsData = await adminApiClient.getStats()
      setStats(statsData)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch data")
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchData()
  }, [])

  // Filter servers based on search query and status
  useEffect(() => {
    let filtered = servers

    // Filter by status
    if (statusFilter !== "all") {
      filtered = filtered.filter(
        (s) => s._meta?.['io.modelcontextprotocol.registry/official']?.status === statusFilter
      )
    }

    // Filter by search query
    if (searchQuery) {
      const query = searchQuery.toLowerCase()
      filtered = filtered.filter(
        (s) =>
          s.server.name.toLowerCase().includes(query) ||
          s.server.title?.toLowerCase().includes(query) ||
          s.server.description.toLowerCase().includes(query)
      )
    }

    setFilteredServers(filtered)
  }, [searchQuery, statusFilter, servers])

  if (selectedServer) {
    return (
      <ServerDetail
        server={selectedServer}
        onClose={() => setSelectedServer(null)}
        onServerCopied={fetchData}
      />
    )
  }

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-center">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-primary mx-auto mb-4"></div>
          <p className="text-muted-foreground">Loading registry data...</p>
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-center">
          <div className="text-red-500 text-6xl mb-4">⚠️</div>
          <h2 className="text-xl font-bold mb-2">Error Loading Registry</h2>
          <p className="text-muted-foreground mb-4">{error}</p>
          <Button onClick={fetchData}>Retry</Button>
        </div>
      </div>
    )
  }

  return (
    <main className="min-h-screen bg-background">
      <div className="border-b">
        <div className="container mx-auto px-6 py-6">
          <div className="flex items-center justify-between mb-6">
            <div>
              <h1 className="text-3xl font-bold mb-2">agentregistry admin</h1>
              <p className="text-muted-foreground">
                Manage your agentregistry
              </p>
            </div>
            <Button
              variant="outline"
              size="icon"
              onClick={fetchData}
              title="Refresh data"
            >
              <RefreshCw className="h-5 w-5" />
            </Button>
          </div>

          {/* Stats */}
          {stats && (
            <div className="grid gap-4 md:grid-cols-5 mb-6">
              <Card className="p-4">
                <div className="flex items-center gap-3">
                  <div className="p-2 bg-primary/10 rounded-lg">
                    <Server className="h-5 w-5 text-primary" />
                  </div>
                  <div>
                    <p className="text-2xl font-bold">{stats.total_servers}</p>
                    <p className="text-xs text-muted-foreground">Total Versions</p>
                  </div>
                </div>
              </Card>

              <Card className="p-4">
                <div className="flex items-center gap-3">
                  <div className="p-2 bg-blue-600/10 rounded-lg">
                    <Database className="h-5 w-5 text-blue-600" />
                  </div>
                  <div>
                    <p className="text-2xl font-bold">{stats.total_server_names}</p>
                    <p className="text-xs text-muted-foreground">Unique Servers</p>
                  </div>
                </div>
              </Card>

              <Card className="p-4">
                <div className="flex items-center gap-3">
                  <div className="p-2 bg-green-600/10 rounded-lg">
                    <CheckCircle className="h-5 w-5 text-green-600" />
                  </div>
                  <div>
                    <p className="text-2xl font-bold">{stats.active_servers}</p>
                    <p className="text-xs text-muted-foreground">Active</p>
                  </div>
                </div>
              </Card>

              <Card className="p-4">
                <div className="flex items-center gap-3">
                  <div className="p-2 bg-yellow-600/10 rounded-lg">
                    <Archive className="h-5 w-5 text-yellow-600" />
                  </div>
                  <div>
                    <p className="text-2xl font-bold">{stats.deprecated_servers}</p>
                    <p className="text-xs text-muted-foreground">Deprecated</p>
                  </div>
                </div>
              </Card>

              <Card className="p-4">
                <div className="flex items-center gap-3">
                  <div className="p-2 bg-red-600/10 rounded-lg">
                    <XCircle className="h-5 w-5 text-red-600" />
                  </div>
                  <div>
                    <p className="text-2xl font-bold">{stats.deleted_servers}</p>
                    <p className="text-xs text-muted-foreground">Deleted</p>
                  </div>
                </div>
              </Card>
            </div>
          )}
        </div>
      </div>

      <div className="container mx-auto px-6 py-8">
        {/* Search and Filters */}
        <div className="flex flex-col md:flex-row items-start md:items-center gap-4 mb-8">
          <div className="relative flex-1 max-w-md">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
            <Input
              placeholder="Search servers..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="pl-10"
            />
          </div>

          {/* Status Filter */}
          <div className="flex gap-2">
            <Button
              variant={statusFilter === "all" ? "default" : "outline"}
              size="sm"
              onClick={() => setStatusFilter("all")}
            >
              All
            </Button>
            <Button
              variant={statusFilter === "active" ? "default" : "outline"}
              size="sm"
              onClick={() => setStatusFilter("active")}
            >
              Active
            </Button>
            <Button
              variant={statusFilter === "deprecated" ? "default" : "outline"}
              size="sm"
              onClick={() => setStatusFilter("deprecated")}
            >
              Deprecated
            </Button>
            <Button
              variant={statusFilter === "deleted" ? "default" : "outline"}
              size="sm"
              onClick={() => setStatusFilter("deleted")}
            >
              Deleted
            </Button>
          </div>

          <div className="flex gap-2">
            <Button
              variant="outline"
              className="gap-2"
              onClick={() => setAddServerDialogOpen(true)}
            >
              <Plus className="h-4 w-4" />
              Add Server
            </Button>
            <Button
              variant="default"
              className="gap-2"
              onClick={() => setImportDialogOpen(true)}
            >
              <Download className="h-4 w-4" />
              Import Servers
            </Button>
          </div>
        </div>

        {/* Server List */}
        <div>
          <h2 className="text-lg font-semibold mb-4">
            Servers
            <span className="text-muted-foreground ml-2">
              ({filteredServers.length})
            </span>
          </h2>

          {filteredServers.length === 0 ? (
            <Card className="p-12">
              <div className="text-center text-muted-foreground">
                <Server className="w-12 h-12 mx-auto mb-4 opacity-50" />
                <p className="text-lg font-medium mb-2">
                  {servers.length === 0
                    ? "No servers in registry"
                    : "No servers match your filters"}
                </p>
                <p className="text-sm mb-4">
                  {servers.length === 0
                    ? "Import servers from external registries to get started"
                    : "Try adjusting your search or filter criteria"}
                </p>
                {servers.length === 0 && (
                  <Button
                    variant="outline"
                    className="gap-2"
                    onClick={() => setImportDialogOpen(true)}
                  >
                    <Download className="h-4 w-4" />
                    Import Servers
                  </Button>
                )}
              </div>
            </Card>
          ) : (
            <div className="grid gap-4">
              {filteredServers.map((server, index) => (
                <ServerCard
                  key={`${server.server.name}-${server.server.version}-${index}`}
                  server={server}
                  onClick={setSelectedServer}
                />
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Import Dialog */}
      <ImportDialog
        open={importDialogOpen}
        onOpenChange={setImportDialogOpen}
        onImportComplete={fetchData}
      />

      {/* Add Server Dialog */}
      <AddServerDialog
        open={addServerDialogOpen}
        onOpenChange={setAddServerDialogOpen}
        onServerAdded={fetchData}
      />
    </main>
  )
}
