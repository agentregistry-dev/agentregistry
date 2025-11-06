"use client"

import { useParams, useRouter } from "next/navigation"
import { Card } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Bot } from "lucide-react"

export default function AgentDetailPage() {
  const params = useParams()
  const router = useRouter()
  const agentId = decodeURIComponent(params.id as string)

  return (
    <div className="min-h-screen bg-background">
      <div className="container mx-auto px-6 py-6">
        <Card className="p-12">
          <div className="text-center">
            <Bot className="w-16 h-16 mx-auto mb-4 text-blue-600" />
            <h2 className="text-2xl font-bold mb-2">Agent Detail View</h2>
            <p className="text-muted-foreground mb-4">
              This feature is coming soon. Agent ID: {agentId}
            </p>
            <Button onClick={() => router.push("/?tab=agents")}>
              Back to Agents
            </Button>
          </div>
        </Card>
      </div>
    </div>
  )
}

