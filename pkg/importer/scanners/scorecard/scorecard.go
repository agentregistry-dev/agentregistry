// Package scorecard implements an OpenSSF Scorecard scanner on the
// pkg/importer.Scanner interface. The engine wrapper
// (runScorecardLibrary + setScorecardTokenEnv) was moved here from
// internal/registry/importer/scorecard_lib.go in the previous commit;
// this file adds the Scanner struct + Config + result translation
// that fits the v1alpha1 enrichment surface.
package scorecard

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ossf/scorecard/v4/checker"
	scorechecks "github.com/ossf/scorecard/v4/checks"
	"github.com/ossf/scorecard/v4/clients"
	docchecks "github.com/ossf/scorecard/v4/docs/checks"
	sclog "github.com/ossf/scorecard/v4/log"
	scpkg "github.com/ossf/scorecard/v4/pkg"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/importer"
)

// ScannerName is the `source` column value used in
// v1alpha1.enrichment_findings for rows produced by this scanner.
const ScannerName = "scorecard"

// runFunc is the seam that lets tests substitute a fake scorecard
// engine. Prod code uses runScorecardLibrary (below).
type runFunc func(ctx context.Context, owner, repo, token string) (aggregate float64, headSHA string, checks []checker.CheckResult, err error)

// Config carries scanner construction knobs. Zero-value Config works
// — public Scorecard defaults + unauthenticated GitHub (subject to
// the 60 req/hr limit; pass GitHubToken in CI to avoid throttling).
type Config struct {
	// GitHubToken authenticates the Scorecard library's GitHub calls.
	// Empty uses whatever GITHUB_AUTH_TOKEN / GITHUB_TOKEN / GH_TOKEN
	// / GH_AUTH_TOKEN are already in the environment.
	GitHubToken string
	// Timeout overrides the per-scan deadline. Defaults to the
	// legacy runScorecardLibrary value (30s).
	Timeout time.Duration
	// HighlightLimit caps how many failing checks turn into
	// Findings. Defaults to 5 to match the legacy behavior.
	HighlightLimit int

	// run is an internal test hook. Unexported so callers can't
	// accidentally swap the production engine.
	run runFunc
}

// Scanner is the OpenSSF Scorecard implementation of
// pkg/importer.Scanner.
type Scanner struct {
	cfg Config
}

// New constructs a Scorecard scanner with the supplied config. Zero
// values fill in the legacy defaults (30s timeout, 5 highlights,
// production runScorecardLibrary engine).
func New(cfg Config) *Scanner {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.HighlightLimit == 0 {
		cfg.HighlightLimit = 5
	}
	if cfg.run == nil {
		cfg.run = runScorecardLibraryDetailed
	}
	return &Scanner{cfg: cfg}
}

// Name returns "scorecard".
func (s *Scanner) Name() string { return ScannerName }

// Supports returns true for Agent, MCPServer, or Skill with a
// GitHub repository URL. Scorecard only speaks GitHub in remote
// mode so other SCMs return false. Dispatch is via the shared
// importer.GitHubRepoFor helper.
func (s *Scanner) Supports(obj v1alpha1.Object) bool {
	_, _, ok := importer.GitHubRepoFor(obj)
	return ok
}

// Scan runs the configured engine against obj's Repository URL and
// translates the response into annotations + labels + findings.
func (s *Scanner) Scan(ctx context.Context, obj v1alpha1.Object) (importer.ScanResult, error) {
	owner, repo, ok := importer.GitHubRepoFor(obj)
	if !ok {
		return importer.ScanResult{}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()

	aggregate, headSHA, checks, err := s.cfg.run(ctx, owner, repo, s.cfg.GitHubToken)
	if err != nil {
		return importer.ScanResult{}, fmt.Errorf("scorecard: %w", err)
	}
	return buildResult(aggregate, headSHA, checks, s.cfg.HighlightLimit), nil
}

// repoFor + parseGitHubRepoSplit were moved to
// pkg/importer/githubrepo.go (importer.GitHubRepoFor) so the OSV
// and Scorecard scanners share one resolution path. git blame for
// the deleted verbatim bodies traces back through the
// `git mv scorecard_lib.go` commit.

// -----------------------------------------------------------------------------
// Result assembly (new for v1alpha1 enrichment surface)
// -----------------------------------------------------------------------------

// buildResult translates the engine output into a ScanResult.
func buildResult(aggregate float64, headSHA string, checks []checker.CheckResult, highlightLimit int) importer.ScanResult {
	scoreStr := strconv.FormatFloat(aggregate, 'f', 1, 64)
	bucket := classifyBucket(aggregate)

	annos := map[string]string{
		importer.AnnoScorecardScore:  scoreStr,
		importer.AnnoScorecardBucket: bucket,
	}
	if headSHA != "" {
		annos[importer.AnnoScorecardRef] = headSHA
	}
	labels := map[string]string{
		importer.AnnoScorecardBucket: bucket,
	}
	return importer.ScanResult{
		Annotations: annos,
		Labels:      labels,
		Findings:    pickFindings(checks, highlightLimit),
	}
}

// classifyBucket collapses a scorecard aggregate onto a letter
// bucket. Thresholds match the scorecard-visualizer tool + the
// decision in V1ALPHA1_IMPORTER_ENRICHMENT.md.
func classifyBucket(score float64) string {
	switch {
	case score >= 8.0:
		return "A"
	case score >= 6.0:
		return "B"
	case score >= 4.0:
		return "C"
	case score >= 2.0:
		return "D"
	default:
		return "F"
	}
}

// pickFindings returns one Finding per failing check, sorted
// ascending by score (worst first), capped at highlightLimit (0 =
// uncapped). Filter logic matches the legacy
// extractScorecardHighlights (see below): checks scoring
// MaxResultScore (fully passed) or < 0 (inconclusive) are excluded.
func pickFindings(results []checker.CheckResult, limit int) []importer.Finding {
	entries := make([]checker.CheckResult, 0, len(results))
	for _, c := range results {
		if c.Score < 0 || c.Score >= checker.MaxResultScore {
			continue
		}
		entries = append(entries, c)
	}
	if len(entries) == 0 {
		return nil
	}
	slices.SortFunc(entries, func(a, b checker.CheckResult) int {
		if a.Score == b.Score {
			return cmp.Compare(a.Name, b.Name)
		}
		return cmp.Compare(a.Score, b.Score)
	})
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	out := make([]importer.Finding, 0, len(entries))
	for _, c := range entries {
		reason := strings.TrimSpace(c.Reason)
		if len(reason) > 200 {
			reason = reason[:197] + "..."
		}
		out = append(out, importer.Finding{
			Severity: severityForScore(c.Score),
			ID:       c.Name,
			Data: map[string]any{
				"score":  c.Score,
				"reason": reason,
			},
		})
	}
	return out
}

// severityForScore maps a scorecard 0–10 check score onto the
// importer's ordinal severity buckets. Scorecard is "higher is
// better", inverted from CVSS; we flip it so a 0/10 check lands as
// "high" (urgent repo hygiene issue).
func severityForScore(score int) string {
	switch {
	case score <= 2:
		return "high"
	case score <= 5:
		return "medium"
	default:
		return "low"
	}
}

// runScorecardLibraryDetailed is the richer sibling of
// runScorecardLibrary used by Scanner.Scan. Returns the aggregate
// score, HEAD SHA, and raw CheckResult list so pickFindings can emit
// structured Findings per failing check.
//
// Body mirrors the legacy runScorecardLibrary (below) — same
// Scorecard library calls, same token env dance — just with a
// richer return. Kept as a sibling function (rather than modifying
// runScorecardLibrary) so the legacy entry point is preserved
// byte-for-byte under git blame.
func runScorecardLibraryDetailed(ctx context.Context, owner, repo, token string) (float64, string, []checker.CheckResult, error) {
	repoURL := fmt.Sprintf("github.com/%s/%s", owner, repo)

	if restore := setScorecardTokenEnv(strings.TrimSpace(token)); restore != nil {
		defer restore()
	}

	logger := sclog.NewLogger(sclog.WarnLevel)
	repoRef, repoClient, ossFuzzClient, ciiClient, vulnClient, err := checker.GetClients(ctx, repoURL, "", logger)
	if err != nil {
		return 0, "", nil, err
	}
	defer func() { _ = repoClient.Close() }()
	if ossFuzzClient != nil {
		defer func() { _ = ossFuzzClient.Close() }()
	}

	checksToRun := scorechecks.GetAll()
	result, err := scpkg.RunScorecard(ctx, repoRef, clients.HeadSHA, 0, checksToRun, repoClient, ossFuzzClient, ciiClient, vulnClient)
	if err != nil {
		return 0, "", nil, err
	}
	checkDocs, err := docchecks.Read()
	if err != nil {
		return 0, "", nil, err
	}
	aggregate, err := result.GetAggregateScore(checkDocs)
	if err != nil {
		return 0, "", nil, err
	}
	if aggregate == checker.InconclusiveResultScore {
		aggregate = 0
	}
	return aggregate, result.Repo.CommitSHA, result.Checks, nil
}

// Compile-time assertion that *Scanner satisfies importer.Scanner.
var _ importer.Scanner = (*Scanner)(nil)

// -----------------------------------------------------------------------------
// Legacy pre-Scanner helpers. runScorecardLibrary +
// extractScorecardHighlights are removed — Scanner.Scan +
// pickFindings + runScorecardLibraryDetailed above replace them,
// and their old caller in the legacy importer was disabled in the
// rename commit. git blame continuity is via that commit.
//
// setScorecardTokenEnv is kept byte-identical and used by
// runScorecardLibraryDetailed.
// -----------------------------------------------------------------------------

func setScorecardTokenEnv(token string) func() {
	if token == "" {
		return nil
	}
	originals := map[string]*string{}
	for _, key := range []string{"GITHUB_AUTH_TOKEN", "GITHUB_TOKEN", "GH_TOKEN", "GH_AUTH_TOKEN"} {
		if val, exists := os.LookupEnv(key); exists {
			copy := val
			originals[key] = &copy
		} else {
			originals[key] = nil
		}
		_ = os.Setenv(key, token)
	}
	return func() {
		for key, val := range originals {
			if val == nil {
				_ = os.Unsetenv(key)
			} else {
				_ = os.Setenv(key, *val)
			}
		}
	}
}
