const PLATFORM_DISPLAY: Record<string, { label: string; description: string }> = {
  local: { label: "Local", description: "Docker Compose on this machine" },
  kubernetes: { label: "Kubernetes", description: "Kubernetes cluster" },
  openshell: { label: "OpenShell", description: "Secure sandboxed runtime" },
}

export function platformDisplayName(platform: string): string {
  return PLATFORM_DISPLAY[platform]?.label ?? platform
}

export function platformDescription(platform: string): string {
  return PLATFORM_DISPLAY[platform]?.description ?? ""
}
