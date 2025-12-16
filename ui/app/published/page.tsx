"use client"

import { useEffect, useState } from "react"
import Link from "next/link"
import { Card } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { adminApiClient, ServerResponse, SkillResponse, AgentResponse } from "@/lib/admin-api"
import { Trash2, AlertCircle, Calendar, Package, Rocket } from "lucide-react"
import { toast } from "sonner"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

type DeploymentResponse = {
  serverName: string
  version: string
  deployedAt: string
  updatedAt: string
  status: string
  config: Record<string, string>
  preferRemote: boolean
  resourceType: string
}

export default function PublishedPage() {
  const [servers, setServers] = useState<ServerResponse[]>([])
  const [skills, setSkills] = useState<SkillResponse[]>([])
  const [agents, setAgents] = useState<AgentResponse[]>([])
  const [deployments, setDeployments] = useState<DeploymentResponse[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [unpublishing, setUnpublishing] = useState(false)
  const [deploying, setDeploying] = useState(false)
  const [itemToUnpublish, setItemToUnpublish] = useState<{ name: string, version: string, type: 'server' | 'skill' | 'agent' } | null>(null)
  const [itemToDeploy, setItemToDeploy] = useState<{ name: string, version: string, type: 'server' | 'agent' } | null>(null)

  const fetchPublished = async () => {
    try {
      setLoading(true)
      setError(null)
      
      // Fetch all published servers (with pagination if needed)
      const allServers: ServerResponse[] = []
      let serverCursor: string | undefined
      
      do {
        const response = await adminApiClient.listPublishedServers({ 
          cursor: serverCursor, 
          limit: 100,
        })
        allServers.push(...response.servers)
        serverCursor = response.metadata.nextCursor
      } while (serverCursor)
      
      setServers(allServers)

      // Fetch all published skills (with pagination if needed)
      const allSkills: SkillResponse[] = []
      let skillCursor: string | undefined
      
      do {
        const response = await adminApiClient.listPublishedSkills({ 
          cursor: skillCursor, 
          limit: 100,
        })
        allSkills.push(...response.skills)
        skillCursor = response.metadata.nextCursor
      } while (skillCursor)
      
      setSkills(allSkills)

      // Fetch all published agents (with pagination if needed)
      const allAgents: AgentResponse[] = []
      let agentCursor: string | undefined
      
      do {
        const response = await adminApiClient.listPublishedAgents({ 
          cursor: agentCursor, 
          limit: 100,
        })
        allAgents.push(...response.agents)
        agentCursor = response.metadata.nextCursor
      } while (agentCursor)
      
      setAgents(allAgents)

      // Fetch deployments to check what's currently deployed
      const deploymentData = await adminApiClient.listDeployments()
      setDeployments(deploymentData)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch published resources')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchPublished()
    // Refresh every 30 seconds
    const interval = setInterval(fetchPublished, 30000)
    return () => clearInterval(interval)
  }, [])

  // Check if a resource is currently deployed (check both name and version)
  const isDeployed = (name: string, version: string) => {
    return deployments.some(d => d.serverName === name && d.version === version)
  }

  const handleUnpublish = async (name: string, version: string, type: 'server' | 'skill' | 'agent') => {
    // Check if the resource is deployed (check specific version)
    if (type !== 'skill' && isDeployed(name, version)) {
      toast.error(`Cannot unpublish ${name} version ${version} while it's deployed. Remove it from the Deployed page first.`)
      return
    }
    setItemToUnpublish({ name, version, type })
  }

  const handleDeploy = async (name: string, version: string, type: 'server' | 'agent') => {
    setItemToDeploy({ name, version, type })
  }

  const confirmDeploy = async () => {
    if (!itemToDeploy) return

    try {
      setDeploying(true)
      
      // Deploy server or agent
      await adminApiClient.deployServer({
        serverName: itemToDeploy.name,
        version: itemToDeploy.version,
        config: {},
        preferRemote: false,
        resourceType: itemToDeploy.type === 'agent' ? 'agent' : 'mcp',
      })
      
      setItemToDeploy(null)
      toast.success(`Successfully deployed ${itemToDeploy.name}!`)
      // Refresh deployments to update the UI
      await fetchPublished()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to deploy resource')
    } finally {
      setDeploying(false)
    }
  }

  const confirmUnpublish = async () => {
    if (!itemToUnpublish) return

    try {
      setUnpublishing(true)
      
      if (itemToUnpublish.type === 'server') {
        await adminApiClient.unpublishServerStatus(itemToUnpublish.name, itemToUnpublish.version)
        setServers(prev => prev.filter(s => s.server.name !== itemToUnpublish.name || s.server.version !== itemToUnpublish.version))
      } else if (itemToUnpublish.type === 'skill') {
        await adminApiClient.unpublishSkillStatus(itemToUnpublish.name, itemToUnpublish.version)
        setSkills(prev => prev.filter(s => s.skill.name !== itemToUnpublish.name || s.skill.version !== itemToUnpublish.version))
      } else if (itemToUnpublish.type === 'agent') {
        await adminApiClient.unpublishAgentStatus(itemToUnpublish.name, itemToUnpublish.version)
        setAgents(prev => prev.filter(a => a.agent.name !== itemToUnpublish.name || a.agent.version !== itemToUnpublish.version))
      }
      
      setItemToUnpublish(null)
      toast.success(`Successfully unpublished ${itemToUnpublish.name}`)
      // Refresh deployments to update the UI
      await fetchPublished()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to unpublish resource')
    } finally {
      setUnpublishing(false)
    }
  }

  const totalPublished = servers.length + skills.length + agents.length
  
  return (
    <main className="min-h-screen bg-background">
      {/* Stats Section */}
      <div className="bg-muted/30 border-b">
        <div className="container mx-auto px-6 py-6">
          <div className="grid gap-4 md:grid-cols-4">
            <Card className="p-4 hover:shadow-md transition-all duration-200 border hover:border-primary/20">
              <div className="flex items-center gap-3">
                <div className="p-2 bg-green-500/10 rounded-lg flex items-center justify-center">
                  <svg
                    xmlns="http://www.w3.org/2000/svg"
                    fill="none"
                    viewBox="0 0 24 24"
                    strokeWidth={2}
                    stroke="currentColor"
                    className="h-5 w-5 text-green-600"
                  >
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      d="M9 12.75L11.25 15 15 9.75M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                    />
                  </svg>
                </div>
                <div>
                  <p className="text-2xl font-bold">{totalPublished}</p>
                  <p className="text-xs text-muted-foreground">Total Published</p>
                </div>
              </div>
            </Card>

            <Card className="p-4 hover:shadow-md transition-all duration-200 border hover:border-primary/20">
              <div className="flex items-center gap-3">
                <div className="p-2 bg-blue-500/10 rounded-lg flex items-center justify-center">
                  <svg
                    xmlns="http://www.w3.org/2000/svg"
                    fill="none"
                    viewBox="0 0 24 24"
                    strokeWidth={2}
                    stroke="currentColor"
                    className="h-5 w-5 text-blue-600"
                  >
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      d="M5.25 14.25h13.5m-13.5 0a3 3 0 01-3-3m3 3a3 3 0 100 6h13.5a3 3 0 100-6m-16.5-3a3 3 0 013-3h13.5a3 3 0 013 3m-19.5 0a4.5 4.5 0 01.9-2.7L5.737 5.1a3.375 3.375 0 012.7-1.35h7.126c1.062 0 2.062.5 2.7 1.35l2.587 3.45a4.5 4.5 0 01.9 2.7m0 0a3 3 0 01-3 3m0 3h.008v.008h-.008v-.008zm0-6h.008v.008h-.008v-.008zm-3 6h.008v.008h-.008v-.008zm0-6h.008v.008h-.008v-.008z"
                    />
                  </svg>
                </div>
                <div>
                  <p className="text-2xl font-bold">{servers.length}</p>
                  <p className="text-xs text-muted-foreground">MCP Servers</p>
                </div>
              </div>
            </Card>

            <Card className="p-4 hover:shadow-md transition-all duration-200 border hover:border-primary/20">
              <div className="flex items-center gap-3">
                <div className="p-2 bg-yellow-500/10 rounded-lg flex items-center justify-center">
                  <svg
                    xmlns="http://www.w3.org/2000/svg"
                    fill="none"
                    viewBox="0 0 24 24"
                    strokeWidth={2}
                    stroke="currentColor"
                    className="h-5 w-5 text-yellow-600"
                  >
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      d="M3.75 13.5l10.5-11.25L12 10.5h8.25L9.75 21.75 12 13.5H3.75z"
                    />
                  </svg>
                </div>
                <div>
                  <p className="text-2xl font-bold">{skills.length}</p>
                  <p className="text-xs text-muted-foreground">Skills</p>
                </div>
              </div>
            </Card>

            <Card className="p-4 hover:shadow-md transition-all duration-200 border hover:border-primary/20">
              <div className="flex items-center gap-3">
                <div className="p-2 bg-purple-500/10 rounded-lg flex items-center justify-center">
                  <svg
                    xmlns="http://www.w3.org/2000/svg"
                    fill="none"
                    viewBox="0 0 24 24"
                    strokeWidth={2}
                    stroke="currentColor"
                    className="h-5 w-5 text-purple-600"
                  >
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      d="M8.25 3v1.5M4.5 8.25H3m18 0h-1.5M4.5 12H3m18 0h-1.5m-15 3.75H3m18 0h-1.5M8.25 19.5V21M12 3v1.5m0 15V21m3.75-18v1.5m0 15V21m-9-1.5h10.5a2.25 2.25 0 002.25-2.25V6.75a2.25 2.25 0 00-2.25-2.25H6.75A2.25 2.25 0 004.5 6.75v10.5a2.25 2.25 0 002.25 2.25zm.75-12h9v9h-9v-9z"
                    />
                  </svg>
                </div>
                <div>
                  <p className="text-2xl font-bold">{agents.length}</p>
                  <p className="text-xs text-muted-foreground">Agents</p>
                </div>
              </div>
            </Card>
          </div>
        </div>
      </div>

      <div className="container mx-auto px-6 py-12">
        <div className="max-w-4xl mx-auto">
          <div className="mb-8">
            <h1 className="text-3xl font-bold mb-2">Published Resources</h1>
            <p className="text-muted-foreground">
              View and manage MCP servers, skills, and agents that are currently published in the registry.
            </p>
          </div>

          {error && (
            <Card className="p-4 mb-6 bg-destructive/10 border-destructive/20">
              <div className="flex items-center gap-2 text-destructive">
                <AlertCircle className="h-4 w-4" />
                <p className="text-sm font-medium">{error}</p>
              </div>
            </Card>
          )}

          {loading ? (
            <Card className="p-12">
              <div className="text-center text-muted-foreground">
                <p className="text-lg font-medium">Loading published resources...</p>
              </div>
            </Card>
          ) : totalPublished === 0 ? (
            <Card className="p-12">
              <div className="text-center text-muted-foreground">
                <div className="w-16 h-16 mx-auto mb-4 opacity-50 flex items-center justify-center">
                  <Package className="w-16 h-16" />
                </div>
                <p className="text-lg font-medium mb-2">
                  No published resources
                </p>
                <p className="text-sm mb-6">
                  Publish MCP servers, skills, or agents from the Admin panel to see them here.
                </p>
                <Link
                  href="/"
                  className="inline-flex items-center justify-center rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:opacity-50 disabled:pointer-events-none ring-offset-background bg-primary text-primary-foreground hover:bg-primary/90 h-10 py-2 px-4"
                >
                  Go to Admin Panel
                </Link>
              </div>
            </Card>
          ) : (
            <div className="space-y-4">
              {/* MCP Servers Section */}
              {servers.length > 0 && (
                <>
                  <h2 className="text-xl font-semibold mt-6 mb-3">MCP Servers ({servers.length})</h2>
                  {servers.map((serverResponse) => {
                    const server = serverResponse.server
                    const meta = serverResponse._meta?.['io.modelcontextprotocol.registry/official']
                    const deployed = isDeployed(server.name, server.version)
                    return (
                      <Card key={`${server.name}-${server.version}`} className="p-6 hover:shadow-md transition-all duration-200">
                        <div className="flex items-start justify-between">
                          <div className="flex-1">
                            <div className="flex items-center gap-3 mb-3">
                              <h3 className="text-xl font-semibold">{server.name}</h3>
                            </div>
                            
                            <p className="text-sm text-muted-foreground mb-3">{server.description}</p>
                            
                            <div className="grid grid-cols-2 gap-4 text-sm">
                              <div className="flex items-center gap-2 text-muted-foreground">
                                <Package className="h-4 w-4" />
                                <span>Version: {server.version}</span>
                              </div>
                              {meta?.publishedAt && (
                                <div className="flex items-center gap-2 text-muted-foreground">
                                  <Calendar className="h-4 w-4" />
                                  <span>Published: {new Date(meta.publishedAt).toLocaleDateString()}</span>
                                </div>
                              )}
                            </div>
                          </div>

                          <div className="flex gap-2 ml-4">
                            <Button
                              variant="default"
                              size="sm"
                              onClick={() => handleDeploy(server.name, server.version, 'server')}
                              disabled={deploying || deployed}
                            >
                              <Rocket className="h-4 w-4 mr-2" />
                              {deployed ? 'Already Deployed' : 'Deploy'}
                            </Button>
                            <Button
                              variant="outline"
                              size="sm"
                              onClick={() => handleUnpublish(server.name, server.version, 'server')}
                              disabled={unpublishing || deployed}
                              title={deployed ? 'Remove from Deployed page first' : ''}
                            >
                              Unpublish
                            </Button>
                          </div>
                        </div>
                      </Card>
                    )
                  })}
                </>
              )}

              {/* Skills Section */}
              {skills.length > 0 && (
                <>
                  <h2 className="text-xl font-semibold mt-6 mb-3">Skills ({skills.length})</h2>
                  {skills.map((skillResponse) => {
                    const skill = skillResponse.skill
                    const meta = skillResponse._meta?.['io.modelcontextprotocol.registry/official']
                    return (
                      <Card key={`${skill.name}-${skill.version}`} className="p-6 hover:shadow-md transition-all duration-200">
                        <div className="flex items-start justify-between">
                          <div className="flex-1">
                            <div className="flex items-center gap-3 mb-3">
                              <h3 className="text-xl font-semibold">{skill.title || skill.name}</h3>
                            </div>
                            
                            <p className="text-sm text-muted-foreground mb-3">{skill.description}</p>
                            
                            <div className="grid grid-cols-2 gap-4 text-sm">
                              <div className="flex items-center gap-2 text-muted-foreground">
                                <Package className="h-4 w-4" />
                                <span>Version: {skill.version}</span>
                              </div>
                              {meta?.publishedAt && (
                                <div className="flex items-center gap-2 text-muted-foreground">
                                  <Calendar className="h-4 w-4" />
                                  <span>Published: {new Date(meta.publishedAt).toLocaleDateString()}</span>
                                </div>
                              )}
                            </div>
                          </div>

                          <Button
                            variant="outline"
                            size="sm"
                            className="ml-4"
                            onClick={() => handleUnpublish(skill.name, skill.version, 'skill')}
                            disabled={unpublishing}
                          >
                            Unpublish
                          </Button>
                        </div>
                      </Card>
                    )
                  })}
                </>
              )}

              {/* Agents Section */}
              {agents.length > 0 && (
                <>
                  <h2 className="text-xl font-semibold mt-6 mb-3">Agents ({agents.length})</h2>
                  {agents.map((agentResponse) => {
                    const agent = agentResponse.agent
                    const meta = agentResponse._meta?.['io.modelcontextprotocol.registry/official']
                    return (
                      <Card key={`${agent.name}-${agent.version}`} className="p-6 hover:shadow-md transition-all duration-200">
                        <div className="flex items-start justify-between">
                          <div className="flex-1">
                            <div className="flex items-center gap-3 mb-3">
                              <h3 className="text-xl font-semibold">{agent.name}</h3>
                            </div>
                            
                            <p className="text-sm text-muted-foreground mb-3">{agent.description}</p>
                            
                            <div className="grid grid-cols-2 gap-4 text-sm">
                              <div className="flex items-center gap-2 text-muted-foreground">
                                <Package className="h-4 w-4" />
                                <span>Version: {agent.version}</span>
                              </div>
                              {meta?.publishedAt && (
                                <div className="flex items-center gap-2 text-muted-foreground">
                                  <Calendar className="h-4 w-4" />
                                  <span>Published: {new Date(meta.publishedAt).toLocaleDateString()}</span>
                                </div>
                              )}
                            </div>
                          </div>

                          <div className="flex gap-2 ml-4">
                            <Button
                              variant="default"
                              size="sm"
                              onClick={() => handleDeploy(agent.name, agent.version, 'agent')}
                              disabled={deploying}
                            >
                              <Rocket className="h-4 w-4 mr-2" />
                              Deploy
                            </Button>
                            <Button
                              variant="outline"
                              size="sm"
                              onClick={() => handleUnpublish(agent.name, agent.version, 'agent')}
                              disabled={unpublishing}
                            >
                              Unpublish
                            </Button>
                          </div>
                        </div>
                      </Card>
                    )
                  })}
                </>
              )}
            </div>
          )}
        </div>
      </div>

      {/* Unpublish Confirmation Dialog */}
      <Dialog open={!!itemToUnpublish} onOpenChange={(open) => !open && setItemToUnpublish(null)}>
        <DialogContent onClose={() => setItemToUnpublish(null)}>
          <DialogHeader>
            <DialogTitle>Unpublish Resource</DialogTitle>
            <DialogDescription>
              Are you sure you want to unpublish <strong>{itemToUnpublish?.name}</strong> (version {itemToUnpublish?.version})?
              <br />
              <br />
              This will change its status to unpublished. The resource will still exist in the registry but won&apos;t be visible to public users.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setItemToUnpublish(null)}
              disabled={unpublishing}
            >
              Cancel
            </Button>
            <Button
              variant="default"
              onClick={confirmUnpublish}
              disabled={unpublishing}
            >
              {unpublishing ? 'Unpublishing...' : 'Unpublish'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Deploy Confirmation Dialog */}
      <Dialog open={!!itemToDeploy} onOpenChange={(open) => !open && setItemToDeploy(null)}>
        <DialogContent onClose={() => setItemToDeploy(null)}>
          <DialogHeader>
            <DialogTitle>Deploy Resource</DialogTitle>
            <DialogDescription>
              Deploy <strong>{itemToDeploy?.name}</strong> (version {itemToDeploy?.version})?
              <br />
              <br />
              This will start the {itemToDeploy?.type === 'server' ? 'MCP server' : 'agent'} on your system. It will be accessible through the gateway at port 21212.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setItemToDeploy(null)}
              disabled={deploying}
            >
              Cancel
            </Button>
            <Button
              variant="default"
              onClick={confirmDeploy}
              disabled={deploying}
            >
              {deploying ? 'Deploying...' : 'Deploy'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </main>
  )
}

