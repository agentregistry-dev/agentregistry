// DNS-1123 subdomain form: lowercase alphanumeric, hyphens, and dots.
// Must start and end with alphanumeric. Each dot-separated segment is a
// DNS-1123 label (1-63 chars). Total length 1-253. Mirrors
// pkg/api/v1alpha1.DNSSubdomainPattern so the UI rejects names client-side
// with the same shape the backend enforces.
export const DNS_SUBDOMAIN_RE =
    /^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?(\.[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?)*$/

export const DNS_SUBDOMAIN_MAX_LEN = 253

export const DNS_SUBDOMAIN_HELP =
    "Lowercase alphanumeric, hyphens, and dots; max 253 characters; each dot-separated segment must start and end with alphanumeric (max 63 chars per segment)."

export function isValidDNSSubdomain(s: string): boolean {
    return s.length > 0 && s.length <= DNS_SUBDOMAIN_MAX_LEN && DNS_SUBDOMAIN_RE.test(s)
}
