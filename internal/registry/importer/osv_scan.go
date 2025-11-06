package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// osvPackageQuery represents one package@version to query in OSV.
type osvPackageQuery struct {
	Package struct {
		Name      string `json:"name"`
		Ecosystem string `json:"ecosystem"`
	} `json:"package"`
	Version string `json:"version"`
}

type osvBatchRequest struct {
	Queries []osvPackageQuery `json:"queries"`
}

type osvBatchResponse struct {
	Results []struct {
		Vulns []struct {
			ID       string `json:"id"`
			Severity []struct {
				Type  string `json:"type"`
				Score string `json:"score"`
			} `json:"severity,omitempty"`
		} `json:"vulns"`
	} `json:"results"`
}

type osvScanResult struct {
	Summary string
	Details []string
}

// runOSVScan fetches basic manifests from the repo root and queries OSV for npm, pip, and go.
func (s *Service) runOSVScan(ctx context.Context, owner, repo string) (*osvScanResult, error) {
	timeout := 30 * time.Second
	if s.httpClient != nil && s.httpClient.Timeout > 0 {
		timeout = s.httpClient.Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Try to fetch manifests at repo root via GitHub contents API
	pkgLock, _ := s.fetchRepoContentFile(ctx, owner, repo, "package-lock.json")
	reqTxt, _ := s.fetchRepoContentFile(ctx, owner, repo, "requirements.txt")
	goMod, _ := s.fetchRepoContentFile(ctx, owner, repo, "go.mod")

	var queries []osvPackageQuery
	if len(pkgLock) > 0 {
		queries = append(queries, parseNPMLockForOSV(pkgLock)...)
	}
	if len(reqTxt) > 0 {
		queries = append(queries, parsePipRequirementsForOSV(reqTxt)...)
	}
	if len(goMod) > 0 {
		queries = append(queries, parseGoModForOSV(goMod)...)
	}
	if len(queries) == 0 {
		return &osvScanResult{Summary: "osv: none"}, nil
	}

	// Deduplicate identical queries
	dedup := map[string]osvPackageQuery{}
	for _, q := range queries {
		key := q.Package.Ecosystem + "|" + q.Package.Name + "|" + q.Version
		dedup[key] = q
	}
	queries = make([]osvPackageQuery, 0, len(dedup))
	for _, q := range dedup {
		queries = append(queries, q)
	}

	vulnsPerIndex, ids, totals, err := s.queryOSVBatch(ctx, queries)
	if err != nil {
		return nil, err
	}

	// Count by ecosystem
	npmCount, pipCount, goCount := 0, 0, 0
	details := []string{}
	for i, q := range queries {
		vcount := vulnsPerIndex[i]
		if vcount == 0 {
			continue
		}
		switch q.Package.Ecosystem {
		case "npm":
			npmCount += vcount
		case "PyPI":
			pipCount += vcount
		case "Go":
			goCount += vcount
		}
		// include up to 2 IDs per package for detail
		idlist := ids[i]
		if len(idlist) > 2 {
			idlist = idlist[:2]
		}
		details = append(details, fmt.Sprintf("%s@%s (%s): %s", q.Package.Name, q.Version, q.Package.Ecosystem, strings.Join(idlist, ", ")))
		if len(details) > 50 {
			break
		}
	}

	summary := fmt.Sprintf("osv: npm=%d, pip=%d, go=%d", npmCount, pipCount, goCount)
	if totals != nil {
		sum := totals.Critical + totals.High + totals.Medium
		if sum > 0 {
			summary = fmt.Sprintf("%s; severity: critical=%d, high=%d, medium=%d", summary, totals.Critical, totals.High, totals.Medium)
		}
	}
	return &osvScanResult{Summary: summary, Details: details}, nil
}

func parseNPMLockForOSV(data []byte) []osvPackageQuery {
	type lockPkg struct {
		Version string `json:"version"`
		Name    string `json:"name"`
	}
	type lockV2 struct {
		Packages     map[string]lockPkg `json:"packages"`
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	var v lockV2
	_ = json.Unmarshal(data, &v)
	queries := []osvPackageQuery{}
	// packages object (v2)
	for path, p := range v.Packages {
		if p.Version == "" {
			continue
		}
		name := p.Name
		if name == "" {
			// derive from path: node_modules/<name>
			segs := strings.Split(path, "/")
			if len(segs) > 0 {
				name = segs[len(segs)-1]
			}
		}
		if name == "" {
			continue
		}
		q := osvPackageQuery{}
		q.Package.Name = name
		q.Package.Ecosystem = "npm"
		q.Version = p.Version
		queries = append(queries, q)
		if len(queries) > 400 { // limit payload size
			break
		}
	}
	// dependencies map (older structure)
	for name, dep := range v.Dependencies {
		if dep.Version == "" {
			continue
		}
		q := osvPackageQuery{}
		q.Package.Name = name
		q.Package.Ecosystem = "npm"
		q.Version = dep.Version
		queries = append(queries, q)
		if len(queries) > 800 {
			break
		}
	}
	return queries
}

func parsePipRequirementsForOSV(data []byte) []osvPackageQuery {
	lines := strings.Split(string(data), "\n")
	queries := []osvPackageQuery{}
	re := regexp.MustCompile(`^\s*([A-Za-z0-9_.\-]+)\s*==\s*([0-9][^\s#]+)`) // pkg==ver
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := re.FindStringSubmatch(line)
		if len(m) == 3 {
			q := osvPackageQuery{}
			q.Package.Name = strings.ToLower(m[1])
			q.Package.Ecosystem = "PyPI"
			q.Version = m[2]
			queries = append(queries, q)
		}
		if len(queries) > 400 {
			break
		}
	}
	return queries
}

func parseGoModForOSV(data []byte) []osvPackageQuery {
	lines := strings.Split(string(data), "\n")
	queries := []osvPackageQuery{}
	re := regexp.MustCompile(`^\s*require\s+([^\s]+)\s+v([0-9][^\s]+)`) // require module vX
	inBlock := false
	blockRe := regexp.MustCompile(`^\s*([^-\s][^\s]+)\s+v([0-9][^\s]+)`)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "require (") {
			inBlock = true
			continue
		}
		if inBlock {
			if strings.HasPrefix(line, ")") {
				inBlock = false
				continue
			}
			m := blockRe.FindStringSubmatch(line)
			if len(m) == 3 {
				q := osvPackageQuery{}
				q.Package.Name = m[1]
				q.Package.Ecosystem = "Go"
				q.Version = "v" + m[2]
				queries = append(queries, q)
			}
			continue
		}
		m := re.FindStringSubmatch(line)
		if len(m) == 3 {
			q := osvPackageQuery{}
			q.Package.Name = m[1]
			q.Package.Ecosystem = "Go"
			q.Version = "v" + m[2]
			queries = append(queries, q)
		}
		if len(queries) > 400 {
			break
		}
	}
	return queries
}

type osvSeverityTotals struct {
	Critical int
	High     int
	Medium   int
}

func (s *Service) queryOSVBatch(ctx context.Context, queries []osvPackageQuery) ([]int, [][]string, *osvSeverityTotals, error) {
	body, _ := json.Marshal(osvBatchRequest{Queries: queries})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.osv.dev/v1/querybatch", strings.NewReader(string(body)))
	if err != nil {
		return nil, nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, nil, fmt.Errorf("osv status %d", resp.StatusCode)
	}
	var br osvBatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&br); err != nil {
		return nil, nil, nil, err
	}
	counts := make([]int, len(queries))
	ids := make([][]string, len(queries))
	totals := &osvSeverityTotals{}
	for i := range counts {
		if i >= len(br.Results) {
			continue
		}
		r := br.Results[i]
		counts[i] = len(r.Vulns)
		for _, v := range r.Vulns {
			ids[i] = append(ids[i], v.ID)
			// try to parse severity score when available
			for _, sev := range v.Severity {
				if sev.Score == "" {
					continue
				}
				// scores are typically numeric strings, e.g., "7.8"
				if f, err := strconv.ParseFloat(sev.Score, 64); err == nil {
					switch {
					case f >= 9.0:
						totals.Critical++
					case f >= 7.0:
						totals.High++
					case f >= 4.0:
						totals.Medium++
					}
				}
			}
		}
	}
	return counts, ids, totals, nil
}
