"use client"

import { useEffect, useState } from "react"
import Link from "next/link"
import { Card } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { ServerCard } from "@/components/server-card"
import { ServerDetail } from "@/components/server-detail"
import { DeleteConfirmationDialog } from "@/components/delete-confirmation-dialog"
import { adminApiClient, ServerResponse } from "@/lib/admin-api"
import MCPIcon from "@/components/icons/mcp"
import { Search, Zap, Bot, Settings } from "lucide-react"
import { Button } from "@/components/ui/button"

export default function RegistryPage() {
  const [activeTab, setActiveTab] = useState("servers")
  const [servers, setServers] = useState<ServerResponse[]>([])
  const [filteredServers, setFilteredServers] = useState<ServerResponse[]>([])
  const [searchQuery, setSearchQuery] = useState("")
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [serverToDelete, setServerToDelete] = useState<ServerResponse | null>(null)
  const [selectedServer, setSelectedServer] = useState<ServerResponse | null>(null)

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
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch data")
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchData()
  }, [])

  // Filter servers based on search query
  useEffect(() => {
    let filtered = servers

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
  }, [searchQuery, servers])

  // Handle server deletion - open dialog
  const handleDeleteServer = (server: ServerResponse) => {
    setServerToDelete(server)
    setDeleteDialogOpen(true)
  }

  // Confirm and execute deletion
  const confirmDeleteServer = async () => {
    if (!serverToDelete) return

    const serverKey = `${serverToDelete.server.name}@${serverToDelete.server.version}`

    try {
      setDeleting(true)
      await adminApiClient.deleteServer(serverToDelete.server.name, serverToDelete.server.version)
      
      // Remove from local state
      setServers(servers.filter(s => 
        `${s.server.name}@${s.server.version}` !== serverKey
      ))
      
      // Close dialog
      setDeleteDialogOpen(false)
      setServerToDelete(null)
    } catch (err) {
      alert(err instanceof Error ? err.message : "Failed to delete server")
    } finally {
      setDeleting(false)
    }
  }

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-center">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-primary mx-auto mb-4"></div>
          <p className="text-muted-foreground">Loading registry...</p>
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

  // Show server detail view if a server is selected
  if (selectedServer) {
    return (
      <ServerDetail
        server={selectedServer}
        onClose={() => setSelectedServer(null)}
        onServerCopied={fetchData}
      />
    )
  }

  return (
    <main className="min-h-screen bg-background">
      {/* Header */}
      <div className="border-b">
        <div className="container mx-auto px-6 py-6">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-6">
              <img 
                src="/ui/arlogo.png" 
                alt="Agent Registry" 
                width={200} 
                height={67}
              />
              <div className="flex items-center gap-4 text-sm">
                <Link href="/registry" className="text-foreground font-medium">
                  Registry
                </Link>
                <Link href="/" className="text-muted-foreground hover:text-foreground transition-colors flex items-center gap-2">
                  <Settings className="h-4 w-4" />
                  Admin
                </Link>
              </div>
            </div>
          </div>
        </div>
      </div>

      <div className="container mx-auto px-6 py-8">
        {/* Global Search */}
        <div className="mb-8">
          <div className="relative max-w-2xl mx-auto">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-5 w-5 text-muted-foreground" />
            <Input
              placeholder="Search servers, skills, agents..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="pl-10 h-12 text-base"
            />
          </div>
        </div>

        {/* Stats */}
        <div className="grid gap-4 md:grid-cols-3 mb-12 max-w-4xl mx-auto">
          <Card className="p-6 text-center hover:shadow-md transition-all duration-200 border hover:border-primary/20">
            <div className="flex flex-col items-center gap-2">
              <div className="p-3 bg-primary/10 rounded-lg">
                <span className="h-6 w-6 text-primary flex items-center justify-center">
                  <MCPIcon />
                </span>
              </div>
              <div>
                <p className="text-3xl font-bold">{servers.length}</p>
                <p className="text-sm text-muted-foreground">MCP Servers</p>
              </div>
            </div>
          </Card>

          <Card className="p-6 text-center hover:shadow-md transition-all duration-200 border hover:border-primary/20">
            <div className="flex flex-col items-center gap-2">
              <div className="p-3 bg-primary/20 rounded-lg">
                <Zap className="h-6 w-6 text-primary" />
              </div>
              <div>
                <p className="text-3xl font-bold">0</p>
                <p className="text-sm text-muted-foreground">Skills</p>
              </div>
            </div>
          </Card>

          <Card className="p-6 text-center hover:shadow-md transition-all duration-200 border hover:border-primary/20">
            <div className="flex flex-col items-center gap-2">
              <div className="p-3 bg-primary/30 rounded-lg">
                <Bot className="h-6 w-6 text-primary" />
              </div>
              <div>
                <p className="text-3xl font-bold">0</p>
                <p className="text-sm text-muted-foreground">Agents</p>
              </div>
            </div>
          </Card>
        </div>

        {/* Content Tabs */}
        <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full">
          <TabsList className="mb-8 mx-auto flex w-fit">
            <TabsTrigger value="servers" className="gap-2">
              <span className="h-4 w-4 flex items-center justify-center">
                <MCPIcon />
              </span>
              Servers ({filteredServers.length})
            </TabsTrigger>
            <TabsTrigger value="skills" className="gap-2">
              <Zap className="h-4 w-4" />
              Skills (0)
            </TabsTrigger>
            <TabsTrigger value="agents" className="gap-2">
              <Bot className="h-4 w-4" />
              Agents (0)
            </TabsTrigger>
          </TabsList>

          {/* Servers Tab */}
          <TabsContent value="servers">
            {filteredServers.length === 0 ? (
              <Card className="p-12">
                <div className="text-center text-muted-foreground">
                  <div className="w-12 h-12 mx-auto mb-4 opacity-50 flex items-center justify-center">
                    <MCPIcon />
                  </div>
                  <p className="text-lg font-medium mb-2">
                    {servers.length === 0
                      ? "No servers in registry"
                      : "No servers match your search"}
                  </p>
                  <p className="text-sm">
                    {servers.length === 0
                      ? "Check back later for new servers"
                      : "Try adjusting your search criteria"}
                  </p>
                </div>
              </Card>
            ) : (
              <div className="grid gap-4 max-w-5xl mx-auto">
                {filteredServers.map((server, index) => (
                  <ServerCard
                    key={`${server.server.name}-${server.server.version}-${index}`}
                    server={server}
                    showDelete={true}
                    onDelete={handleDeleteServer}
                    showExternalLinks={false}
                    onClick={() => setSelectedServer(server)}
                  />
                ))}
              </div>
            )}
          </TabsContent>

          {/* Skills Tab */}
          <TabsContent value="skills">
            <Card className="p-12">
              <div className="text-center text-muted-foreground">
                <div className="w-12 h-12 mx-auto mb-4 opacity-50 flex items-center justify-center text-primary">
                  <Zap className="w-12 h-12" />
                </div>
                <p className="text-lg font-medium mb-2">No skills available yet</p>
                <p className="text-sm">
                  Skills will be available soon
                </p>
              </div>
            </Card>
          </TabsContent>

          {/* Agents Tab */}
          <TabsContent value="agents">
            <Card className="p-12">
              <div className="text-center text-muted-foreground">
                <div className="w-12 h-12 mx-auto mb-4 opacity-50 flex items-center justify-center text-primary">
                  <Bot className="w-12 h-12" />
                </div>
                <p className="text-lg font-medium mb-2">No agents available yet</p>
                <p className="text-sm">
                  Agents will be available soon
                </p>
              </div>
            </Card>
          </TabsContent>
        </Tabs>
      </div>

      {/* Delete Confirmation Dialog */}
      <DeleteConfirmationDialog
        open={deleteDialogOpen}
        onOpenChange={setDeleteDialogOpen}
        onConfirm={confirmDeleteServer}
        title="Remove Server from Registry"
        itemName={serverToDelete?.server.title || serverToDelete?.server.name || ""}
        isDeleting={deleting}
      />
    </main>
  )
}

