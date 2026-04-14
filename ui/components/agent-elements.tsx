interface AgentElementsProps {
  extraElements: Record<string, unknown>
}

function formatElementLabel(elementName: string) {
  return elementName
    .replace(/[-_]+/g, ' ')
    .replace(/\b\w/g, (char) => char.toUpperCase())
}

export function AgentElements({ extraElements }: AgentElementsProps) {
  const extraElementEntries = Object.entries(extraElements).sort(([left], [right]) => left.localeCompare(right))

  if (extraElementEntries.length === 0) {
    return null
  }

  return (
    <section>
      <h3 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground mb-3">Additional Elements</h3>
      <div className="space-y-4">
        {extraElementEntries.map(([elementName, elementValue]) => (
          <div key={elementName}>
            <p className="text-xs text-muted-foreground mb-2">{formatElementLabel(elementName)}</p>
            <pre className="bg-muted p-3 rounded-md overflow-x-auto text-xs leading-relaxed">
              {JSON.stringify(elementValue, null, 2)}
            </pre>
          </div>
        ))}
      </div>
    </section>
  )
}
