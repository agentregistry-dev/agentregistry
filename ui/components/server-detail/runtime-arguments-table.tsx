"use client"

import { Badge } from "@/components/ui/badge"
import { Terminal } from "lucide-react"

interface RuntimeArgument {
  type: "named" | "positional"
  name?: string
  value?: string
  description?: string
  isRepeated?: boolean
  valueHint?: string
}

interface RuntimeArgumentsTableProps {
  arguments: RuntimeArgument[]
}

export function RuntimeArgumentsTable({ arguments: runtimeArguments }: RuntimeArgumentsTableProps) {
  if (!runtimeArguments || runtimeArguments.length === 0) {
    return null
  }

  return (
    <div className="mb-4">
      <h5 className="text-sm font-semibold mb-3 flex items-center gap-2">
        <Terminal className="h-4 w-4 text-primary" />
        Runtime Arguments
      </h5>
      <div className="border rounded-lg overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-muted/50">
            <tr>
              <th className="text-left p-3 font-semibold">Name</th>
              <th className="text-left p-3 font-semibold">Type</th>
              <th className="text-left p-3 font-semibold">Value</th>
              <th className="text-left p-3 font-semibold">Description</th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {runtimeArguments.map((arg, idx) => (
              <tr key={idx} className="hover:bg-muted/30">
                <td className="p-3">
                  {arg.name ? (
                    <code className="px-2 py-1 bg-muted rounded text-xs font-mono">
                      {arg.name}
                    </code>
                  ) : (
                    <span className="text-muted-foreground text-xs">-</span>
                  )}
                </td>
                <td className="p-3">
                  <Badge variant={arg.type === 'named' ? 'default' : 'secondary'} className="text-xs">
                    {arg.type}
                  </Badge>
                </td>
                <td className="p-3">
                  <div className="flex items-center gap-2 flex-wrap">
                    {arg.value && (
                      <code className="px-2 py-1 bg-muted rounded text-xs font-mono">
                        {arg.value}
                      </code>
                    )}
                    {arg.isRepeated && (
                      <Badge variant="outline" className="text-xs">repeated</Badge>
                    )}
                    {arg.valueHint && (
                      <span className="text-xs text-muted-foreground">({arg.valueHint})</span>
                    )}
                    {!arg.value && !arg.isRepeated && !arg.valueHint && (
                      <span className="text-muted-foreground text-xs">-</span>
                    )}
                  </div>
                </td>
                <td className="p-3">
                  {arg.description ? (
                    <span className="text-xs text-muted-foreground">{arg.description}</span>
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

