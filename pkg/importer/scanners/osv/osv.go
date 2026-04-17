// Package osv implements the OSV.dev vulnerability scanner as a
// pkg/importer.Scanner plug-in. The fetch + parse + query logic
// (parseNPMLockForOSV / parsePipRequirementsForOSV /
// parseGoModForOSV + queryOSVBatch + fetchRepoContentFile) was
// moved here from internal/registry/importer/osv_scan.go in the
// previous commit; this file adds the Scanner struct + Config +
// result translation that fits the v1alpha1 enrichment surface.
package osv

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/importer"
)

// ScannerName is the `source` column value used in
// v1alpha1.enrichment_findings for rows produced by this scanner.
const ScannerName = "osv"

// Config carries the knobs a Scanner needs at construction. Zero
// values select the public OSV + GitHub endpoints and an http.Client
// with a 30s timeout — the defaults the legacy osv_scan used.
type Config struct {
	// HTTPClient is used for every outbound request.
	HTTPClient *http.Client
	// GitHubToken authenticates manifest fetches. Optional; unauth'd
	// GitHub has a 60 req/hr limit which is tight for batch imports.
	GitHubToken string
	// OSVEndpoint overrides https://api.osv.dev/v1/querybatch for
	// tests. Must speak the OSV batch protocol.
	OSVEndpoint string
	// GitHubContentsEndpoint overrides
	// https://api.github.com/repos/{owner}/{repo}/contents/{path}.
	// Tests point this at a local httptest server. The literal
	// "{owner}", "{repo}", "{path}" placeholders are substituted
	// per request.
	GitHubContentsEndpoint string
}

// Scanner is the OSV implementation of pkg/importer.Scanner.
type Scanner struct {
	cfg Config
}

// New constructs an OSV scanner. A nil HTTPClient is replaced by an
// http.Client with a 30s timeout; empty endpoints default to the
// public OSV + GitHub URLs.
func New(cfg Config) *Scanner {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.OSVEndpoint == "" {
		cfg.OSVEndpoint = "https://api.osv.dev/v1/querybatch"
	}
	if cfg.GitHubContentsEndpoint == "" {
		cfg.GitHubContentsEndpoint = "https://api.github.com/repos/{owner}/{repo}/contents/{path}"
	}
	return &Scanner{cfg: cfg}
}

// Name returns "osv".
func (s *Scanner) Name() string { return ScannerName }

// Supports reports true when obj is an Agent, MCPServer, or Skill
// with a Repository URL that parses as a GitHub repo. Legacy
// osv_scan only supported GitHub; the wrap preserves that scope.
// Dispatch is via the shared importer.GitHubRepoFor helper.
func (s *Scanner) Supports(obj v1alpha1.Object) bool {
	_, _, ok := importer.GitHubRepoFor(obj)
	return ok
}

// Scan runs the same fetch → parse → OSV-batch flow the legacy
// runOSVScan did (using the parsers + queryOSVBatch still defined
// below) and translates the response into annotations + labels +
// findings.
func (s *Scanner) Scan(ctx context.Context, obj v1alpha1.Object) (importer.ScanResult, error) {
	owner, repo, ok := importer.GitHubRepoFor(obj)
	if !ok {
		return importer.ScanResult{}, nil
	}

	pkgLock, _ := fetchRepoContentFile(ctx, s.cfg.HTTPClient, s.cfg.GitHubContentsEndpoint, s.cfg.GitHubToken, owner, repo, "package-lock.json")
	reqTxt, _ := fetchRepoContentFile(ctx, s.cfg.HTTPClient, s.cfg.GitHubContentsEndpoint, s.cfg.GitHubToken, owner, repo, "requirements.txt")
	goMod, _ := fetchRepoContentFile(ctx, s.cfg.HTTPClient, s.cfg.GitHubContentsEndpoint, s.cfg.GitHubToken, owner, repo, "go.mod")

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
		return cleanResult(), nil
	}

	// Dedup identical queries — same dedup pass the legacy flow did
	// (order not preserved; OSV batch doesn't care).
	dedup := map[string]osvPackageQuery{}
	for _, q := range queries {
		key := q.Package.Ecosystem + "|" + q.Package.Name + "|" + q.Version
		dedup[key] = q
	}
	queries = queries[:0]
	for _, q := range dedup {
		queries = append(queries, q)
	}

	perQuery, totals, err := queryOSVBatchDetailed(ctx, s.cfg.HTTPClient, s.cfg.OSVEndpoint, queries)
	if err != nil {
		return importer.ScanResult{}, fmt.Errorf("osv: %w", err)
	}
	return buildResult(queries, perQuery, totals), nil
}

// repoFor + parseGitHubRepo were moved to pkg/importer/githubrepo.go
// (importer.GitHubRepoFor) so the OSV and Scorecard scanners share
// one resolution path. git blame for the deleted verbatim bodies
// traces back through the `git mv osv_scan.go` commit.

// -----------------------------------------------------------------------------
// Result assembly (new for v1alpha1 enrichment surface)
// -----------------------------------------------------------------------------

// cleanResult is returned when there's nothing to scan (no manifests
// found at the repo root). Emits zero-count annotations + "clean"
// status so the UI shows a confirmed-scanned resource rather than a
// blank.
func cleanResult() importer.ScanResult {
	return importer.ScanResult{
		Annotations: map[string]string{
			importer.AnnoOSVStatus:        "clean",
			importer.AnnoOSVCountCritical: "0",
			importer.AnnoOSVCountHigh:     "0",
			importer.AnnoOSVCountMedium:   "0",
			importer.AnnoOSVCountLow:      "0",
		},
		Labels: map[string]string{
			importer.AnnoOSVStatus: "clean",
		},
	}
}

// buildResult turns the per-query OSV results into annotations,
// labels (osv-status promoted), and per-CVE findings.
func buildResult(queries []osvPackageQuery, perQuery [][]osvVulnDetail, totals severityCounts) importer.ScanResult {
	status := "clean"
	for _, vs := range perQuery {
		if len(vs) > 0 {
			status = "vulnerable"
			break
		}
	}
	annos := map[string]string{
		importer.AnnoOSVStatus:        status,
		importer.AnnoOSVCountCritical: strconv.Itoa(totals.critical),
		importer.AnnoOSVCountHigh:     strconv.Itoa(totals.high),
		importer.AnnoOSVCountMedium:   strconv.Itoa(totals.medium),
		importer.AnnoOSVCountLow:      strconv.Itoa(totals.low),
	}
	labels := map[string]string{
		importer.AnnoOSVStatus: status,
	}

	var findings []importer.Finding
	for i, vs := range perQuery {
		if len(vs) == 0 {
			continue
		}
		q := queries[i]
		for _, v := range vs {
			findings = append(findings, importer.Finding{
				Severity: classifySeverityScore(v.maxScore),
				ID:       v.id,
				Data: map[string]any{
					"package":   q.Package.Name,
					"version":   q.Version,
					"ecosystem": q.Package.Ecosystem,
					"score":     v.maxScore,
				},
			})
		}
	}
	return importer.ScanResult{
		Annotations: annos,
		Labels:      labels,
		Findings:    findings,
	}
}

// classifySeverityScore maps a CVSS numeric to the ordinal severity
// buckets used in the findings table. Same thresholds as the legacy
// queryOSVBatch totals accumulator (critical ≥ 9, high ≥ 7,
// medium ≥ 4). A negative score means "no parseable severity" —
// surfaced as Low so the CVE stays visible.
func classifySeverityScore(score float64) string {
	switch {
	case score >= 9.0:
		return "critical"
	case score >= 7.0:
		return "high"
	case score >= 4.0:
		return "medium"
	case score >= 0:
		return "low"
	default:
		return "low"
	}
}

// osvVulnDetail is the structured per-CVE record that
// queryOSVBatchDetailed returns alongside the legacy totals. The
// legacy queryOSVBatch (still defined below) returns only ID lists
// per query and aggregate severity totals; Scanner.Scan needs the
// per-CVE max score to emit one Finding per vuln with correct
// severity.
type osvVulnDetail struct {
	id       string
	maxScore float64
}

type severityCounts struct {
	critical, high, medium, low int
}

// queryOSVBatchDetailed is queryOSVBatch's richer sibling used by
// Scanner.Scan. Issues the same POST as queryOSVBatch but retains
// per-CVE severity so findings can be emitted per-finding.
//
// Kept in this file (rather than replacing queryOSVBatch) so the
// legacy-ported queryOSVBatch body stays byte-identical and
// discoverable via git blame.
func queryOSVBatchDetailed(ctx context.Context, client *http.Client, endpoint string, queries []osvPackageQuery) ([][]osvVulnDetail, severityCounts, error) {
	body, _ := json.Marshal(osvBatchRequest{Queries: queries})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, severityCounts{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, severityCounts{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, severityCounts{}, fmt.Errorf("osv status %d", resp.StatusCode)
	}
	var br osvBatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&br); err != nil {
		return nil, severityCounts{}, err
	}

	out := make([][]osvVulnDetail, len(queries))
	var totals severityCounts
	for i := range out {
		if i >= len(br.Results) {
			continue
		}
		for _, v := range br.Results[i].Vulns {
			var best float64 = -1
			for _, sev := range v.Severity {
				if sev.Score == "" {
					continue
				}
				if f, err := strconv.ParseFloat(sev.Score, 64); err == nil && f > best {
					best = f
				}
			}
			out[i] = append(out[i], osvVulnDetail{id: v.ID, maxScore: best})
			switch {
			case best >= 9.0:
				totals.critical++
			case best >= 7.0:
				totals.high++
			case best >= 4.0:
				totals.medium++
			case best >= 0:
				totals.low++
			default:
				totals.low++
			}
		}
	}
	return out, totals, nil
}

// Compile-time assertion that *Scanner satisfies importer.Scanner.
var _ importer.Scanner = (*Scanner)(nil)

// -----------------------------------------------------------------------------
// Legacy (pre-Scanner) helpers. Preserved byte-for-byte from the move
// commit so git blame traces back to the original authorship.
// -----------------------------------------------------------------------------

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

// runOSVScan + osvScanResult were the pre-Scanner entry point and
// return type. Scanner.Scan + buildResult replace them; the fetch
// → dedup → batch flow is inlined into Scanner.Scan above.

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

// queryOSVBatch + osvSeverityTotals were the pre-Scanner batch
// helpers. Scanner.Scan uses queryOSVBatchDetailed above (same
// request payload, richer per-CVE return) instead. The legacy flow
// isn't preserved as a callable function because no caller remains
// after this refactor; git blame continuity is via the rename
// commit.

// fetchRepoContentFile reads a single file from the GitHub contents
// API. Body is ported from the legacy importer's
// fetchRepoContentFileWithRename (internal/registry/importer/importer.go)
// minus the rename-detection retry path, which was specific to the
// legacy dedup/reimport flow and not required by a scanner invocation.
//
// The endpoint parameter carries the URL template with literal
// {owner}/{repo}/{path} placeholders, enabling test-time override.
// In production this is the public GitHub URL set by New().
func fetchRepoContentFile(ctx context.Context, client *http.Client, endpoint, githubToken, owner, repo, path string) ([]byte, error) {
	url := strings.NewReplacer("{owner}", owner, "{repo}", repo, "{path}", path).Replace(endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if githubToken != "" {
		req.Header.Set("Authorization", "Bearer "+githubToken)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("content %s status %d", path, resp.StatusCode)
	}
	var payload struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if strings.ToLower(payload.Encoding) == "base64" {
		data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(payload.Content, "\n", ""))
		if err != nil {
			return nil, err
		}
		return data, nil
	}
	// fallback: sometimes API may return raw
	body, _ := io.ReadAll(resp.Body)
	return body, nil
}
