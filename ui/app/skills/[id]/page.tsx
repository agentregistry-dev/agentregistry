"use client"

import { useParams, useRouter } from "next/navigation"
import { Card } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Zap } from "lucide-react"

export default function SkillDetailPage() {
  const params = useParams()
  const router = useRouter()
  const skillId = decodeURIComponent(params.id as string)

  return (
    <div className="min-h-screen bg-background">
      <div className="container mx-auto px-6 py-6">
        <Card className="p-12">
          <div className="text-center">
            <Zap className="w-16 h-16 mx-auto mb-4 text-purple-600" />
            <h2 className="text-2xl font-bold mb-2">Skill Detail View</h2>
            <p className="text-muted-foreground mb-4">
              This feature is coming soon. Skill ID: {skillId}
            </p>
            <Button onClick={() => router.push("/?tab=skills")}>
              Back to Skills
            </Button>
          </div>
        </Card>
      </div>
    </div>
  )
}

