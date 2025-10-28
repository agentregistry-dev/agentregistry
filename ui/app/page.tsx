"use client"

import { useEffect, useState } from "react"
import { Card } from "@/components/ui/card"
import { Server, Package, Database } from "lucide-react"

interface Registry {
  id: number
  name: string
  url: string
  type: string
}

interface MCPServer {
  id: number
  name: string
  title: string
  description: string
  version: string
  installed: boolean
}

interface Skill {
  id: number
  name: string
  description: string
  version: string
  installed: boolean
}

export default function Home() {
  const [registries, setRegistries] = useState<Registry[]>([])
  const [servers, setServers] = useState<MCPServer[]>([])
  const [skills, setSkills] = useState<Skill[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const fetchData = async () => {
      try {
        const [registriesRes, serversRes, skillsRes] = await Promise.all([
          fetch("/api/registries"),
          fetch("/api/servers"),
          fetch("/api/skills"),
        ])

        if (registriesRes.ok) {
          const data = await registriesRes.json()
          setRegistries(data || [])
        }

        if (serversRes.ok) {
          const data = await serversRes.json()
          setServers(data || [])
        }

        if (skillsRes.ok) {
          const data = await skillsRes.json()
          setSkills(data || [])
        }

        setLoading(false)
      } catch (err) {
        setError(err instanceof Error ? err.message : "Failed to fetch data")
        setLoading(false)
      }
    }

    fetchData()
  }, [])

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-center">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-primary mx-auto"></div>
          <p className="mt-4 text-muted-foreground">Loading...</p>
        </div>
      </div>
    )
  }

  return (
    <main className="min-h-screen bg-background">
      <div className="container mx-auto px-4 py-8">
        <header className="mb-8">
          <h1 className="text-4xl font-bold mb-2">arctl</h1>
          <p className="text-muted-foreground">AI Registry and Runtime</p>
        </header>

        {error && (
          <div className="bg-destructive/10 border border-destructive text-destructive px-4 py-3 rounded mb-6">
            {error}
          </div>
        )}

        <div className="grid gap-6 md:grid-cols-3 mb-8">
          <Card className="p-6">
            <div className="flex items-center gap-4">
              <div className="p-3 bg-primary/10 rounded-lg">
                <Database className="h-6 w-6 text-primary" />
              </div>
              <div>
                <p className="text-2xl font-bold">{registries.length}</p>
                <p className="text-sm text-muted-foreground">Registries</p>
              </div>
            </div>
          </Card>

          <Card className="p-6">
            <div className="flex items-center gap-4">
              <div className="p-3 bg-primary/10 rounded-lg">
                <Server className="h-6 w-6 text-primary" />
              </div>
              <div>
                <p className="text-2xl font-bold">{servers.length}</p>
                <p className="text-sm text-muted-foreground">MCP Servers</p>
              </div>
            </div>
          </Card>

          <Card className="p-6">
            <div className="flex items-center gap-4">
              <div className="p-3 bg-primary/10 rounded-lg">
                <Package className="h-6 w-6 text-primary" />
              </div>
              <div>
                <p className="text-2xl font-bold">{skills.length}</p>
                <p className="text-sm text-muted-foreground">Skills</p>
              </div>
            </div>
          </Card>
        </div>

        <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
          <section>
            <h2 className="text-xl font-semibold mb-4">Connected Registries</h2>
            {registries.length === 0 ? (
              <Card className="p-6">
                <p className="text-muted-foreground text-center">
                  No registries connected yet.
                  <br />
                  <span className="text-sm">
                    Run{" "}
                    <code className="bg-muted px-2 py-1 rounded">
                      arctl connect &lt;url&gt; &lt;name&gt;
                    </code>
                  </span>
                </p>
              </Card>
            ) : (
              <div className="space-y-3">
                {registries.map((registry) => (
                  <Card key={registry.id} className="p-4">
                    <h3 className="font-medium">{registry.name}</h3>
                    <p className="text-sm text-muted-foreground truncate">
                      {registry.url}
                    </p>
                    <span className="text-xs bg-secondary px-2 py-1 rounded mt-2 inline-block">
                      {registry.type}
                    </span>
                  </Card>
                ))}
              </div>
            )}
          </section>

          <section>
            <h2 className="text-xl font-semibold mb-4">MCP Servers</h2>
            {servers.length === 0 ? (
              <Card className="p-6">
                <p className="text-muted-foreground text-center">
                  No MCP servers available.
                  <br />
                  <span className="text-sm">Connect a registry first.</span>
                </p>
              </Card>
            ) : (
              <div className="space-y-3">
                {servers.map((server) => (
                  <Card key={server.id} className="p-4">
                    <h3 className="font-medium">{server.title || server.name}</h3>
                    <p className="text-sm text-muted-foreground line-clamp-2">
                      {server.description}
                    </p>
                    <div className="flex items-center gap-2 mt-2">
                      <span className="text-xs bg-secondary px-2 py-1 rounded">
                        v{server.version}
                      </span>
                      {server.installed && (
                        <span className="text-xs bg-primary/10 text-primary px-2 py-1 rounded">
                          Installed
                        </span>
                      )}
                    </div>
                  </Card>
                ))}
              </div>
            )}
          </section>

          <section>
            <h2 className="text-xl font-semibold mb-4">Skills</h2>
            {skills.length === 0 ? (
              <Card className="p-6">
                <p className="text-muted-foreground text-center">
                  No skills available.
                  <br />
                  <span className="text-sm">Connect a registry first.</span>
                </p>
              </Card>
            ) : (
              <div className="space-y-3">
                {skills.map((skill) => (
                  <Card key={skill.id} className="p-4">
                    <h3 className="font-medium">{skill.name}</h3>
                    <p className="text-sm text-muted-foreground line-clamp-2">
                      {skill.description}
                    </p>
                    <div className="flex items-center gap-2 mt-2">
                      <span className="text-xs bg-secondary px-2 py-1 rounded">
                        v{skill.version}
                      </span>
                      {skill.installed && (
                        <span className="text-xs bg-primary/10 text-primary px-2 py-1 rounded">
                          Installed
                        </span>
                      )}
                    </div>
                  </Card>
                ))}
              </div>
            )}
          </section>
        </div>
      </div>
    </main>
  )
}

