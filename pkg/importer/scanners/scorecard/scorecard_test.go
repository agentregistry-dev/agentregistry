//go:build unit

package scorecard

import (
	"context"
	"errors"
	"testing"

	"github.com/ossf/scorecard/v4/checker"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/importer"
)

// withRun constructs a Scanner with the production run hook swapped
// for a caller-supplied stub. Keeps the runFunc seam unexported while
// giving tests a direct handle.
func withRun(t *testing.T, fn runFunc) *Scanner {
	t.Helper()
	return New(Config{run: fn})
}

// -----------------------------------------------------------------------------
// Supports
// -----------------------------------------------------------------------------

func TestSupports_GitHubRepos(t *testing.T) {
	s := New(Config{})
	for _, url := range []string{
		"https://github.com/org/repo",
		"https://github.com/org/repo.git",
		"git@github.com:org/repo.git",
	} {
		obj := &v1alpha1.MCPServer{
			Spec: v1alpha1.MCPServerSpec{
				Repository: &v1alpha1.Repository{URL: url},
			},
		}
		require.True(t, s.Supports(obj), url)
	}
}

func TestSupports_NonGitHubRejected(t *testing.T) {
	s := New(Config{})
	obj := &v1alpha1.MCPServer{
		Spec: v1alpha1.MCPServerSpec{
			Repository: &v1alpha1.Repository{URL: "https://gitlab.com/org/repo"},
		},
	}
	require.False(t, s.Supports(obj))
}

func TestSupports_PromptUnsupported(t *testing.T) {
	require.False(t, New(Config{}).Supports(&v1alpha1.Prompt{}))
}

// -----------------------------------------------------------------------------
// Scan
// -----------------------------------------------------------------------------

func TestScan_Aggregate_EmitsScoreBucketAndLabel(t *testing.T) {
	s := withRun(t, func(ctx context.Context, owner, repo, token string) (float64, string, []checker.CheckResult, error) {
		return 8.3, "sha256:deadbeef", []checker.CheckResult{
			{Name: "Branch-Protection", Score: 10},
			{Name: "Code-Review", Score: 7, Reason: "ok"},
		}, nil
	})
	obj := &v1alpha1.MCPServer{
		Spec: v1alpha1.MCPServerSpec{
			Repository: &v1alpha1.Repository{URL: "https://github.com/org/repo"},
		},
	}
	res, err := s.Scan(context.Background(), obj)
	require.NoError(t, err)

	require.Equal(t, "8.3", res.Annotations[importer.AnnoScorecardScore])
	require.Equal(t, "A", res.Annotations[importer.AnnoScorecardBucket])
	require.Equal(t, "sha256:deadbeef", res.Annotations[importer.AnnoScorecardRef])
	require.Equal(t, "A", res.Labels[importer.AnnoScorecardBucket])
}

func TestScan_FailingChecksBecomeFindings(t *testing.T) {
	s := withRun(t, func(ctx context.Context, owner, repo, token string) (float64, string, []checker.CheckResult, error) {
		return 5.0, "", []checker.CheckResult{
			{Name: "Branch-Protection", Score: 0, Reason: "no protection"},
			{Name: "SAST", Score: 3, Reason: "missing SAST"},
			{Name: "Code-Review", Score: 10, Reason: "passed"}, // excluded
		}, nil
	})
	obj := &v1alpha1.MCPServer{
		Spec: v1alpha1.MCPServerSpec{
			Repository: &v1alpha1.Repository{URL: "https://github.com/org/repo"},
		},
	}
	res, err := s.Scan(context.Background(), obj)
	require.NoError(t, err)

	require.Len(t, res.Findings, 2)
	// Sorted ascending by score — worst first.
	require.Equal(t, "Branch-Protection", res.Findings[0].ID)
	require.Equal(t, "high", res.Findings[0].Severity)
	require.Equal(t, "no protection", res.Findings[0].Data["reason"])

	require.Equal(t, "SAST", res.Findings[1].ID)
	require.Equal(t, "medium", res.Findings[1].Severity)

	require.NotContains(t, []string{res.Findings[0].ID, res.Findings[1].ID}, "Code-Review")
}

func TestScan_HighlightLimitCaps(t *testing.T) {
	failing := make([]checker.CheckResult, 0, 8)
	for i := 0; i < 8; i++ {
		failing = append(failing, checker.CheckResult{Name: "Check" + string(rune('A'+i)), Score: 1})
	}
	s := New(Config{
		HighlightLimit: 3,
		run: func(ctx context.Context, owner, repo, token string) (float64, string, []checker.CheckResult, error) {
			return 1.0, "", failing, nil
		},
	})
	obj := &v1alpha1.MCPServer{
		Spec: v1alpha1.MCPServerSpec{
			Repository: &v1alpha1.Repository{URL: "https://github.com/org/repo"},
		},
	}
	res, err := s.Scan(context.Background(), obj)
	require.NoError(t, err)
	require.Len(t, res.Findings, 3)
}

func TestScan_InconclusiveChecksExcluded(t *testing.T) {
	s := withRun(t, func(ctx context.Context, owner, repo, token string) (float64, string, []checker.CheckResult, error) {
		return 9.0, "", []checker.CheckResult{
			{Name: "CII-Best-Practices", Score: -1}, // inconclusive, excluded
			{Name: "Fuzzing", Score: 2},
		}, nil
	})
	obj := &v1alpha1.MCPServer{
		Spec: v1alpha1.MCPServerSpec{
			Repository: &v1alpha1.Repository{URL: "https://github.com/org/repo"},
		},
	}
	res, err := s.Scan(context.Background(), obj)
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)
	require.Equal(t, "Fuzzing", res.Findings[0].ID)
}

func TestScan_EngineErrorSurfaces(t *testing.T) {
	s := withRun(t, func(ctx context.Context, owner, repo, token string) (float64, string, []checker.CheckResult, error) {
		return 0, "", nil, errors.New("repo not accessible")
	})
	obj := &v1alpha1.MCPServer{
		Spec: v1alpha1.MCPServerSpec{
			Repository: &v1alpha1.Repository{URL: "https://github.com/org/repo"},
		},
	}
	_, err := s.Scan(context.Background(), obj)
	require.Error(t, err)
	require.Contains(t, err.Error(), "scorecard")
}

func TestScan_UnsupportedObjectReturnsEmptyNoError(t *testing.T) {
	// Supports() should have filtered, but Scan must handle a
	// drive-by Prompt gracefully.
	s := withRun(t, func(ctx context.Context, owner, repo, token string) (float64, string, []checker.CheckResult, error) {
		t.Fatal("run should not be invoked for unsupported kind")
		return 0, "", nil, nil
	})
	res, err := s.Scan(context.Background(), &v1alpha1.Prompt{})
	require.NoError(t, err)
	require.Empty(t, res.Annotations)
}

// -----------------------------------------------------------------------------
// Pure functions
// -----------------------------------------------------------------------------

func TestClassifyBucket(t *testing.T) {
	cases := []struct {
		score  float64
		bucket string
	}{
		{10, "A"},
		{8.0, "A"},
		{7.99, "B"},
		{6.0, "B"},
		{5.9, "C"},
		{4.0, "C"},
		{3.9, "D"},
		{2.0, "D"},
		{1.9, "F"},
		{0, "F"},
	}
	for _, c := range cases {
		require.Equal(t, c.bucket, classifyBucket(c.score), "score %v", c.score)
	}
}

func TestSeverityForScore(t *testing.T) {
	require.Equal(t, "high", severityForScore(0))
	require.Equal(t, "high", severityForScore(2))
	require.Equal(t, "medium", severityForScore(3))
	require.Equal(t, "medium", severityForScore(5))
	require.Equal(t, "low", severityForScore(6))
	require.Equal(t, "low", severityForScore(9))
}

func TestName(t *testing.T) {
	require.Equal(t, "scorecard", New(Config{}).Name())
}
