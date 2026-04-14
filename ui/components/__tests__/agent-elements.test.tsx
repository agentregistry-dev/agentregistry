import { render, screen } from "@testing-library/react"
import { describe, it, expect } from "vitest"
import { AgentElements } from "../agent-elements"

describe("AgentElements", () => {
  it("renders nothing when extraElements is empty", () => {
    const { container } = render(<AgentElements extraElements={{}} />)
    expect(container.firstChild).toBeNull()
  })

  it("renders section header when extraElements exist", () => {
    const extraElements = {
      card: { name: "test-agent", version: "1.0.0" },
    }
    render(<AgentElements extraElements={extraElements} />)
    expect(screen.getByText("Additional Elements")).toBeInTheDocument()
  })

  it("formats element labels correctly", () => {
    const extraElements = {
      "custom-element": { value: "test" },
      "snake_case_name": { enabled: true },
      "mixedCamelCase": { flag: false },
    }
    render(<AgentElements extraElements={extraElements} />)
    expect(screen.getByText("Custom Element")).toBeInTheDocument()
    expect(screen.getByText("Snake Case Name")).toBeInTheDocument()
    expect(screen.getByText("MixedCamelCase")).toBeInTheDocument()
  })

  it("renders JSON stringified values", () => {
    const extraElements = {
      card: { name: "agent", version: "1.0.0" },
    }
    render(<AgentElements extraElements={extraElements} />)
    const jsonContent = screen.getByText((content, element) => {
      return element?.tagName === "PRE" && content.includes('"name": "agent"')
    })
    expect(jsonContent).toBeInTheDocument()
  })

  it("renders multiple extra elements in sorted order", () => {
    const extraElements = {
      zebra: { last: true },
      apple: { first: true },
      middle: { mid: true },
    }
    render(<AgentElements extraElements={extraElements} />)

    const labels = screen.getAllByText((content, element) => {
      return element?.className?.includes("text-muted-foreground") &&
             element?.tagName === "P"
    })

    // Should be alphabetically sorted: apple, middle, zebra
    expect(labels[0]).toHaveTextContent("Apple")
    expect(labels[1]).toHaveTextContent("Middle")
    expect(labels[2]).toHaveTextContent("Zebra")
  })

  it("handles complex nested objects", () => {
    const extraElements = {
      deployment: {
        namespace: "production",
        replicas: 3,
        resources: {
          limits: { cpu: "500m", memory: "512Mi" },
          requests: { cpu: "250m", memory: "256Mi" },
        },
      },
    }
    render(<AgentElements extraElements={extraElements} />)
    expect(screen.getByText("Deployment")).toBeInTheDocument()
    const jsonContent = screen.getByText((content, element) => {
      return element?.tagName === "PRE" && content.includes('"namespace": "production"')
    })
    expect(jsonContent).toBeInTheDocument()
  })

  it("handles primitive values", () => {
    const extraElements = {
      simpleString: "test value",
      simpleNumber: 42,
      simpleBoolean: true,
    }
    render(<AgentElements extraElements={extraElements} />)
    expect(screen.getByText("SimpleString")).toBeInTheDocument()
    expect(screen.getByText("SimpleNumber")).toBeInTheDocument()
    expect(screen.getByText("SimpleBoolean")).toBeInTheDocument()
  })

  it("renders with proper CSS classes for styling", () => {
    const extraElements = {
      card: { test: true },
    }
    const { container } = render(<AgentElements extraElements={extraElements} />)

    // Check for section tag
    const section = container.querySelector("section")
    expect(section).toBeInTheDocument()

    // Check for pre tag with proper classes
    const pre = container.querySelector("pre")
    expect(pre).toHaveClass("bg-muted", "p-3", "rounded-md", "overflow-x-auto")
  })
})
