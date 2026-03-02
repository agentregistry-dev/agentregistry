"use client"

import { useState, useEffect } from "react"
import { SkillResponse } from "@/lib/admin-api"
import { Card } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  X,
  Package,
  Calendar,
  Tag,
  ExternalLink,
  GitBranch,
  Globe,
  Code,
  Link,
  ArrowLeft,
  Zap,
} from "lucide-react"

interface SkillDetailProps {
  skill: SkillResponse
  onClose: () => void
}

export function SkillDetail({ skill, onClose }: SkillDetailProps) {
  const [activeTab, setActiveTab] = useState("overview")
  
  const { skill: skillData, _meta } = skill
  const official = _meta?.['io.modelcontextprotocol.registry/official']

  // Handle ESC key to close
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        onClose()
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => {
      window.removeEventListener('keydown', handleKeyDown)
    }
  }, [onClose])

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
    <div className="fixed inset-0 bg-background z-50 overflow-y-auto">
      <div className="container mx-auto px-6 py-6">
        {/* Back Button */}
        <Button
          variant="ghost"
          onClick={onClose}
          className="mb-4 gap-2"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to Skills
        </Button>

        {/* Header */}
        <div className="flex items-center justify-between mb-6">
          <div className="flex items-start gap-4 flex-1">
            <div className="w-16 h-16 rounded bg-primary/10 flex items-center justify-center flex-shrink-0 mt-1">
              <Zap className="h-8 w-8 text-primary" />
            </div>
            <div className="flex-1">
              <div className="flex items-center gap-3 mb-2 flex-wrap">
                <h1 className="text-3xl font-bold">{skillData.title || skillData.name}</h1>
              </div>
              <p className="text-muted-foreground">{skillData.name}</p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Button variant="ghost" size="icon" onClick={onClose}>
              <X className="h-5 w-5" />
            </Button>
          </div>
        </div>

        {/* Quick Info */}
        <div className="flex flex-wrap gap-3 mb-6 text-sm">
          <div className="flex items-center gap-2 px-3 py-2 bg-muted rounded-md">
            <Tag className="h-3.5 w-3.5 text-muted-foreground" />
            <span className="text-muted-foreground">Version:</span>
            <span className="font-medium">{skillData.version}</span>
          </div>

          {official?.publishedAt && (
            <div className="flex items-center gap-2 px-3 py-2 bg-muted rounded-md">
              <Calendar className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-muted-foreground">Published:</span>
              <span className="font-medium">{formatDate(official.publishedAt)}</span>
            </div>
          )}

          {official?.updatedAt && (
            <div className="flex items-center gap-2 px-3 py-2 bg-muted rounded-md">
              <Calendar className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-muted-foreground">Updated:</span>
              <span className="font-medium">{formatDate(official.updatedAt)}</span>
            </div>
          )}

          {skillData.websiteUrl && (
            <a
              href={skillData.websiteUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-center gap-2 px-3 py-2 bg-muted rounded-md hover:bg-muted/80 transition-colors"
            >
              <Globe className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-muted-foreground">Website:</span>
              <span className="font-medium text-blue-600 flex items-center gap-1">
                Visit <ExternalLink className="h-3 w-3" />
              </span>
            </a>
          )}
        </div>

        {/* Detailed Information Tabs */}
        <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full">
          <TabsList>
            <TabsTrigger value="overview">Overview</TabsTrigger>
            {skillData.packages && skillData.packages.length > 0 && (
              <TabsTrigger value="packages">Packages</TabsTrigger>
            )}
            {skillData.remotes && skillData.remotes.length > 0 && (
              <TabsTrigger value="remotes">Remotes</TabsTrigger>
            )}
            <TabsTrigger value="raw">Raw Data</TabsTrigger>
          </TabsList>

          <TabsContent value="overview" className="space-y-4">
            {/* Description */}
            <Card className="p-6">
              <h3 className="text-lg font-semibold mb-4">Description</h3>
              <p className="text-base">{skillData.description}</p>
            </Card>

            {/* Repository */}
            {skillData.repository?.url && (
              <Card className="p-6">
                <h3 className="text-lg font-semibold mb-4 flex items-center gap-2">
                  <GitBranch className="h-5 w-5" />
                  Repository
                </h3>
                <div className="space-y-2">
                  {skillData.repository.source && (
                    <div className="flex items-center justify-between">
                      <span className="text-sm text-muted-foreground">Source</span>
                      <Badge variant="outline">{skillData.repository.source}</Badge>
                    </div>
                  )}
                  <div className="flex items-center justify-between">
                    <span className="text-sm text-muted-foreground">URL</span>
                    <a
                      href={skillData.repository.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-sm text-blue-600 hover:underline flex items-center gap-1"
                    >
                      {skillData.repository.url} <ExternalLink className="h-3 w-3" />
                    </a>
                  </div>
                </div>
              </Card>
            )}
          </TabsContent>

          <TabsContent value="packages" className="space-y-4">
            {skillData.packages && skillData.packages.length > 0 ? (
              <div className="space-y-3">
                {skillData.packages.map((pkg, i) => (
                  <Card key={i} className="p-4">
                    <div className="flex items-start justify-between mb-3">
                      <div className="flex items-center gap-2">
                        <Package className="h-5 w-5 text-primary" />
                        <h4 className="font-semibold">{pkg.identifier}</h4>
                      </div>
                      <Badge variant="outline">{pkg.registryType}</Badge>
                    </div>
                    <div className="space-y-2 text-sm">
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">Version</span>
                        <span className="font-mono">{pkg.version}</span>
                      </div>
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">Transport</span>
                        <span className="font-mono">{pkg.transport?.type || 'N/A'}</span>
                      </div>
                    </div>
                  </Card>
                ))}
              </div>
            ) : (
              <Card className="p-8">
                <p className="text-center text-muted-foreground">No packages defined</p>
              </Card>
            )}
          </TabsContent>

          <TabsContent value="remotes" className="space-y-4">
            {skillData.remotes && skillData.remotes.length > 0 ? (
              <div className="space-y-3">
                {skillData.remotes.map((remote, i) => (
                  <Card key={i} className="p-4">
                    <div className="flex items-start justify-between mb-3">
                      <div className="flex items-center gap-2">
                        <ExternalLink className="h-5 w-5 text-primary" />
                        <h4 className="font-semibold">Remote {i + 1}</h4>
                      </div>
                    </div>
                    {remote.url && (
                      <div className="space-y-2 text-sm">
                        <div className="flex items-center gap-2">
                          <Link className="h-4 w-4 text-muted-foreground" />
                          <a
                            href={remote.url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="text-blue-600 hover:underline break-all"
                          >
                            {remote.url}
                          </a>
                        </div>
                      </div>
                    )}
                  </Card>
                ))}
              </div>
            ) : (
              <Card className="p-8">
                <p className="text-center text-muted-foreground">No remotes defined</p>
              </Card>
            )}
          </TabsContent>

          <TabsContent value="raw">
            <Card className="p-6">
              <div className="flex items-center justify-between mb-4">
                <h3 className="text-lg font-semibold flex items-center gap-2">
                  <Code className="h-5 w-5" />
                  Raw JSON Data
                </h3>
              </div>
              <pre className="bg-muted p-4 rounded-lg overflow-x-auto text-xs">
                {JSON.stringify(skill, null, 2)}
              </pre>
            </Card>
          </TabsContent>
        </Tabs>
      </div>
    </div>
  )
}

