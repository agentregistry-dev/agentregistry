"use client"

import { useEffect, useState } from "react"
import { useParams, useRouter } from "next/navigation"
import { ServerDetail } from "@/components/server-detail"
import { adminApiClient, ServerResponse } from "@/lib/admin-api"
import { Button } from "@/components/ui/button"

export default function ServerDetailPage() {
  const params = useParams()
  const router = useRouter()
  const [server, setServer] = useState<ServerResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Decode the ID from URL (format: name-version)
  const serverId = decodeURIComponent(params.id as string)

  useEffect(() => {
    const fetchServer = async () => {
      try {
        setLoading(true)
        setError(null)

        // Parse the serverId (format: name@version)
        const parts = serverId.split('@')
        if (parts.length !== 2) {
          setError("Invalid server ID format")
          setLoading(false)
          return
        }

        const [name, version] = parts
        
        // Use the direct API to fetch the specific server
        const serverData = await adminApiClient.getServer(name, version)
        setServer(serverData)
      } catch (err) {
        setError(err instanceof Error ? err.message : "Failed to fetch server")
      } finally {
        setLoading(false)
      }
    }

    fetchServer()
  }, [serverId])

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-center">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-primary mx-auto mb-4"></div>
          <p className="text-muted-foreground">Loading server...</p>
        </div>
      </div>
    )
  }

  if (error || !server) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="text-center">
          <div className="text-red-500 text-6xl mb-4">⚠️</div>
          <h2 className="text-xl font-bold mb-2">Server Not Found</h2>
          <p className="text-muted-foreground mb-4">{error || "The requested server could not be found"}</p>
          <Button onClick={() => router.push("/")}>Back to Servers</Button>
        </div>
      </div>
    )
  }

  return (
    <ServerDetail
      server={server}
      onClose={() => router.back()}
      onServerCopied={() => router.push("/")}
    />
  )
}

