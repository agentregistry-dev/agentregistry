import { render, screen } from "@testing-library/react"
import { describe, it, expect } from "vitest"
import { Badge } from "../badge"

describe("Badge", () => {
  it("renders children", () => {
    render(<Badge>Hello</Badge>)
    expect(screen.getByText("Hello")).toBeInTheDocument()
  })

  it("applies default variant classes", () => {
    render(<Badge>Default</Badge>)
    const badge = screen.getByText("Default")
    expect(badge).toHaveClass("bg-primary")
  })

  it("applies secondary variant classes", () => {
    render(<Badge variant="secondary">Secondary</Badge>)
    const badge = screen.getByText("Secondary")
    expect(badge).toHaveClass("bg-secondary")
  })

  it("applies destructive variant classes", () => {
    render(<Badge variant="destructive">Destructive</Badge>)
    const badge = screen.getByText("Destructive")
    expect(badge).toHaveClass("bg-destructive")
  })

  it("applies outline variant classes", () => {
    render(<Badge variant="outline">Outline</Badge>)
    const badge = screen.getByText("Outline")
    expect(badge).toHaveClass("text-foreground")
    expect(badge).not.toHaveClass("bg-primary")
  })

  it("merges custom className", () => {
    render(<Badge className="custom-class">Custom</Badge>)
    const badge = screen.getByText("Custom")
    expect(badge).toHaveClass("custom-class")
  })
})
