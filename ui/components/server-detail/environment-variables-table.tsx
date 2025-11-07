"use client"

import { Badge } from "@/components/ui/badge"
import { Settings, Key, Lock } from "lucide-react"

interface EnvironmentVariable {
  name: string
  description?: string
  default?: string
  isSecret?: boolean
  format?: string
  choices?: string[]
}

interface EnvironmentVariablesTableProps {
  variables: EnvironmentVariable[]
}

export function EnvironmentVariablesTable({ variables }: EnvironmentVariablesTableProps) {
  if (!variables || variables.length === 0) {
    return null
  }

  return (
    <div>
      <h5 className="text-sm font-semibold mb-3 flex items-center gap-2">
        <Settings className="h-4 w-4 text-primary" />
        Environment Variables
      </h5>
      <div className="border rounded-lg overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-muted/50">
            <tr>
              <th className="text-left p-3 font-semibold">Name</th>
              <th className="text-left p-3 font-semibold">Type</th>
              <th className="text-left p-3 font-semibold">Default</th>
              <th className="text-left p-3 font-semibold">Choices</th>
              <th className="text-left p-3 font-semibold">Description</th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {variables.map((envVar, idx) => (
              <tr key={idx} className="hover:bg-muted/30">
                <td className="p-3">
                  <div className="flex items-center gap-2">
                    {envVar.isSecret ? (
                      <Lock className="h-3.5 w-3.5 text-amber-600 flex-shrink-0" />
                    ) : (
                      <Key className="h-3.5 w-3.5 text-muted-foreground flex-shrink-0" />
                    )}
                    <code className="text-xs font-mono font-semibold">{envVar.name}</code>
                  </div>
                </td>
                <td className="p-3">
                  <div className="flex gap-1 flex-wrap">
                    {envVar.isSecret && (
                      <Badge variant="destructive" className="text-xs">Secret</Badge>
                    )}
                    {envVar.format && (
                      <Badge variant="outline" className="text-xs">{envVar.format}</Badge>
                    )}
                    {!envVar.isSecret && !envVar.format && (
                      <span className="text-muted-foreground text-xs">-</span>
                    )}
                  </div>
                </td>
                <td className="p-3">
                  {envVar.default !== undefined ? (
                    <code className="px-2 py-1 bg-muted rounded text-xs font-mono">
                      {envVar.default}
                    </code>
                  ) : (
                    <span className="text-muted-foreground text-xs">-</span>
                  )}
                </td>
                <td className="p-3">
                  {envVar.choices && envVar.choices.length > 0 ? (
                    <div className="flex gap-1 flex-wrap">
                      {envVar.choices.map((choice, cIdx) => (
                        <code key={cIdx} className="px-2 py-1 bg-muted rounded text-xs font-mono">
                          {choice}
                        </code>
                      ))}
                    </div>
                  ) : (
                    <span className="text-muted-foreground text-xs">-</span>
                  )}
                </td>
                <td className="p-3">
                  {envVar.description ? (
                    <span className="text-xs text-muted-foreground">{envVar.description}</span>
                  ) : (
                    <span className="text-muted-foreground text-xs">-</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

