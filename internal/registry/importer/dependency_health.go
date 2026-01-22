package importer

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
)

type dependencyHealthSummary struct {
	TotalPackages       int
	Ecosystems          map[string]int
	CopyleftCount       int
	UnknownLicenseCount int
}

func (d *dependencyHealthSummary) summaryString() string {
	if d == nil || d.TotalPackages == 0 {
		return ""
	}
	parts := []string{fmt.Sprintf("deps: total=%d", d.TotalPackages)}
	tops := topEcosystems(d.Ecosystems, 3)
	if len(tops) > 0 {
		parts = append(parts, fmt.Sprintf("top=%s", strings.Join(tops, ", ")))
	}
	if d.CopyleftCount > 0 {
		parts = append(parts, fmt.Sprintf("copyleft=%d", d.CopyleftCount))
	}
	if d.UnknownLicenseCount > 0 {
		parts = append(parts, fmt.Sprintf("unknown=%d", d.UnknownLicenseCount))
	}
	return strings.Join(parts, " ")
}

func (d *dependencyHealthSummary) detailString() string {
	if d == nil || d.TotalPackages == 0 {
		return ""
	}
	parts := []string{fmt.Sprintf("packages=%d", d.TotalPackages)}
	tops := topEcosystems(d.Ecosystems, 5)
	if len(tops) > 0 {
		parts = append(parts, fmt.Sprintf("ecosystems=%s", strings.Join(tops, ", ")))
	}
	parts = append(parts, fmt.Sprintf("copyleft=%d", d.CopyleftCount))
	parts = append(parts, fmt.Sprintf("unknown_license=%d", d.UnknownLicenseCount))
	return "deps: " + strings.Join(parts, "; ")
}

func topEcosystems(m map[string]int, limit int) []string {
	if len(m) == 0 {
		return nil
	}
	type entry struct {
		Name  string
		Count int
	}
	entries := make([]entry, 0, len(m))
	for name, count := range m {
		entries = append(entries, entry{Name: name, Count: count})
	}
	slices.SortFunc(entries, func(a, b entry) int {
		if a.Count == b.Count {
			return cmp.Compare(a.Name, b.Name)
		}
		return cmp.Compare(b.Count, a.Count)
	})
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	result := make([]string, 0, len(entries))
	for _, e := range entries {
		result = append(result, fmt.Sprintf("%s:%d", e.Name, e.Count))
	}
	return result
}

func (s *Service) fetchDependencyHealthSummary(ctx context.Context, owner, repo string) (*dependencyHealthSummary, error) {
	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/%s/dependency-graph/sbom?ref=HEAD", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if s.githubToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.githubToken)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return nil, nil
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		_ = resp.Body.Close()
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("sbom status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload struct {
		SBOM struct {
			Packages []struct {
				Name                 string   `json:"name"`
				LicenseConcluded     string   `json:"licenseConcluded"`
				LicenseInfoFromFiles []string `json:"licenseInfoFromFiles"`
				ExternalRefs         []struct {
					ReferenceCategory string `json:"referenceCategory"`
					ReferenceType     string `json:"referenceType"`
					ReferenceLocator  string `json:"referenceLocator"`
				} `json:"externalRefs"`
			} `json:"packages"`
		} `json:"sbom"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	_ = resp.Body.Close()
	if len(payload.SBOM.Packages) == 0 {
		return nil, nil
	}
	summary := &dependencyHealthSummary{Ecosystems: map[string]int{}}
	for _, pkg := range payload.SBOM.Packages {
		purlType := detectPurlType(pkg.ExternalRefs)
		if purlType != "" {
			if purlType == "github" {
				continue
			}
			summary.Ecosystems[purlType]++
		}
		if hasCopyleftLicense(pkg.LicenseConcluded, pkg.LicenseInfoFromFiles) {
			summary.CopyleftCount++
		}
		if isLicenseUnknown(pkg.LicenseConcluded, pkg.LicenseInfoFromFiles) {
			summary.UnknownLicenseCount++
		}
		summary.TotalPackages++
	}
	if summary.TotalPackages == 0 {
		return nil, nil
	}
	return summary, nil
}

func detectPurlType(refs []struct {
	ReferenceCategory string `json:"referenceCategory"`
	ReferenceType     string `json:"referenceType"`
	ReferenceLocator  string `json:"referenceLocator"`
}) string {
	for _, ref := range refs {
		if !strings.EqualFold(ref.ReferenceType, "purl") {
			continue
		}
		locator := strings.TrimPrefix(ref.ReferenceLocator, "pkg:")
		if locator == ref.ReferenceLocator {
			continue
		}
		if before, _, found := strings.Cut(locator, "/"); found {
			return strings.ToLower(before)
		}
		if idx := strings.IndexAny(locator, "@?"); idx != -1 {
			return strings.ToLower(locator[:idx])
		}
		return strings.ToLower(locator)
	}
	return ""
}

func hasCopyleftLicense(concluded string, fromFiles []string) bool {
	licenses := gatherLicenses(concluded, fromFiles)
	for _, lic := range licenses {
		slug := strings.ToLower(lic)
		if strings.Contains(slug, "gpl") || strings.Contains(slug, "agpl") || strings.Contains(slug, "lgpl") || strings.Contains(slug, "sspl") || strings.Contains(slug, "copyleft") || strings.Contains(slug, "cc-by-sa") {
			return true
		}
	}
	return false
}

func isLicenseUnknown(concluded string, fromFiles []string) bool {
	licenses := gatherLicenses(concluded, fromFiles)
	return len(licenses) == 0
}

func gatherLicenses(concluded string, fromFiles []string) []string {
	var out []string
	if l := strings.TrimSpace(concluded); l != "" && !strings.EqualFold(l, "NOASSERTION") {
		out = append(out, l)
	}
	for _, entry := range fromFiles {
		l := strings.TrimSpace(entry)
		if l == "" || strings.EqualFold(l, "NOASSERTION") {
			continue
		}
		out = append(out, l)
	}
	return out
}
