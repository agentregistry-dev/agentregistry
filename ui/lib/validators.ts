// DNS-1123 label form: lowercase alphanumeric and hyphens, must start/end
// with alphanumeric, 1-63 chars. Mirrors pkg/api/v1alpha1.DNSLabelPattern so
// the UI rejects names client-side with the same shape the backend enforces.
export const DNS_LABEL_RE = /^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$/

export const DNS_LABEL_HELP =
    "Lowercase alphanumeric and hyphens, max 63 characters; must start and end with alphanumeric."

export function isValidDNSLabel(s: string): boolean {
    return s.length > 0 && s.length <= 63 && DNS_LABEL_RE.test(s)
}
