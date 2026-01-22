package importer

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/ossf/scorecard/v4/checker"
	scorechecks "github.com/ossf/scorecard/v4/checks"
	"github.com/ossf/scorecard/v4/clients"
	docchecks "github.com/ossf/scorecard/v4/docs/checks"
	sclog "github.com/ossf/scorecard/v4/log"
	scpkg "github.com/ossf/scorecard/v4/pkg"
)

// runScorecardLibrary runs OpenSSF Scorecard using the Go library in remote mode.
// Returns the aggregate score (0-10) and up to `limitHighlights` failing check highlights.
func runScorecardLibrary(parent context.Context, owner, repo, githubToken string) (float64, []string, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

	repoURL := fmt.Sprintf("github.com/%s/%s", owner, repo)
	limitHighlights := 5

	restoreEnv := setScorecardTokenEnv(strings.TrimSpace(githubToken))
	if restoreEnv != nil {
		defer restoreEnv()
	}

	logger := sclog.NewLogger(sclog.WarnLevel)
	repoRef, repoClient, ossFuzzClient, ciiClient, vulnClient, err := checker.GetClients(ctx, repoURL, "", logger)
	if err != nil {
		return 0, nil, err
	}
	defer repoClient.Close() //nolint:errcheck
	if ossFuzzClient != nil {
		defer ossFuzzClient.Close() //nolint:errcheck
	}

	checksToRun := scorechecks.GetAll()
	result, err := scpkg.RunScorecard(ctx, repoRef, clients.HeadSHA, 0, checksToRun, repoClient, ossFuzzClient, ciiClient, vulnClient)
	if err != nil {
		return 0, nil, err
	}

	checkDocs, err := docchecks.Read()
	if err != nil {
		return 0, nil, err
	}

	aggregate, err := result.GetAggregateScore(checkDocs)
	if err != nil {
		return 0, nil, err
	}
	if aggregate == checker.InconclusiveResultScore {
		aggregate = 0
	}

	highlights := extractScorecardHighlights(result.Checks, limitHighlights)
	return aggregate, highlights, nil
}

func extractScorecardHighlights(results []checker.CheckResult, limit int) []string {
	if limit <= 0 {
		limit = 5
	}
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
	if len(entries) > limit {
		entries = entries[:limit]
	}
	highlights := make([]string, 0, len(entries))
	for _, c := range entries {
		reason := strings.TrimSpace(c.Reason)
		if len(reason) > 120 {
			reason = reason[:117] + "..."
		}
		if reason != "" {
			highlights = append(highlights, fmt.Sprintf("scorecard: %s=%d/10 (%s)", c.Name, c.Score, reason))
		} else {
			highlights = append(highlights, fmt.Sprintf("scorecard: %s=%d/10", c.Name, c.Score))
		}
	}
	return highlights
}

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
