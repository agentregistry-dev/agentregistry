"use client"

import { useState } from "react"
import { SkillResponse } from "@/lib/admin-api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import {
  Calendar,
  ExternalLink,
  Code,
  Zap,
  Copy,
  Check,
  History,
} from "lucide-react"

interface SkillDetailProps {
  skill: SkillResponse
  allTags?: SkillResponse[]
}

export function SkillDetail({ skill, allTags: allTagsProp }: SkillDetailProps) {
  const [activeTab, setActiveTab] = useState("overview")
  const [jsonCopied, setJsonCopied] = useState(false)
  const [selectedTag, setSelectedTag] = useState<SkillResponse>(skill)

  const allTags = allTagsProp || [skill]

  const { skill: skillData, _meta } = selectedTag
  const official = _meta?.['io.modelcontextprotocol.registry/official']

  const handleTagChange = (tag: string) => {
    const newTag = allTags.find(v => v.skill.tag === tag)
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
            <Zap className="h-6 w-6 text-primary" />
          </div>
          <div className="flex-1 min-w-0">
            <h1 className="text-2xl font-bold truncate mb-1">{skillData.title || skillData.name}</h1>
            <p className="text-[15px] text-muted-foreground">{skillData.name}</p>
          </div>
        </div>

        {/* Tag selector */}
        {allTags.length > 1 && (
          <div className="flex items-center gap-3 px-3 py-2 bg-accent/50 border border-primary/10 rounded-md">
            <History className="h-4 w-4 text-muted-foreground" />
            <span className="text-sm">{allTags.length} tags</span>
            <Select value={selectedTag.skill.tag} onValueChange={handleTagChange}>
              <SelectTrigger className="w-[160px] h-7 text-sm">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {allTags.map((tag) => (
                  <SelectItem key={tag.skill.tag} value={tag.skill.tag}>
                    {tag.skill.tag}
                    {tag.skill.tag === skill.skill.tag && " (latest)"}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}

        {/* Quick info */}
        <div className="flex flex-wrap gap-2">
          <span className="flex items-center gap-1.5 px-2.5 py-1 bg-muted rounded text-sm">
            <span className="font-mono">{skillData.tag}</span>
            {allTags.length > 1 && (
              <Badge variant="secondary" className="text-[10px] px-1 py-0 h-3.5">{allTags.length} total</Badge>
            )}
          </span>
          {official?.publishedAt && (
            <span className="flex items-center gap-1.5 px-2.5 py-1 bg-muted rounded text-sm">
              <Calendar className="h-3 w-3 text-muted-foreground" />
              {formatDate(official.publishedAt)}
            </span>
          )}
        </div>

        <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full">
          <TabsList className="mb-4">
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="raw">Raw</TabsTrigger>
          </TabsList>

          <TabsContent value="overview" className="space-y-6">
            <section>
              <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-2">Description</h3>
              <p className="text-[15px] leading-relaxed">{skillData.description}</p>
            </section>

            {(() => {
              const repoUrl = skillData.source?.repository?.url
              if (!repoUrl) return null
              return (
                <section>
                  <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-2">Repository</h3>
                  <div className="space-y-1.5 text-sm">
                    <div className="flex items-center justify-between text-xs">
                      <span className="text-muted-foreground">URL</span>
                      <a
                        href={repoUrl}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="text-primary hover:underline flex items-center gap-1"
                      >
                        {repoUrl} <ExternalLink className="h-2.5 w-2.5" />
                      </a>
                    </div>
                  </div>
                </section>
              )
            })()}
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
