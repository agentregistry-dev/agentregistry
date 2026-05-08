package frameworks

import (
	"fmt"
)

// LoadOpts configures top-level framework loading.
type LoadOpts struct {
	// StageDir is where embedded frameworks are written. Defaults to a tmp dir
	// when empty. arctl normally creates and cleans this on each run.
	StageDir string
	// UserDir is the user-level framework directory (typically UserFrameworksDir()).
	// When empty, the user source is skipped.
	UserDir string
	// ProjectRoot is the current project root for project-local frameworks (typically
	// the cwd or the resolved project dir). When empty, the project source is skipped.
	ProjectRoot string
}

// LoadAll discovers frameworks from project-local, user, and embedded sources
// (in that order) and returns a populated Registry. Conflicts are recorded
// but not raised as errors; callers can surface r.Conflicts() to the user.
func LoadAll(opts LoadOpts) (*Registry, error) {
	r := NewRegistry()

	// 1. Project-local
	if opts.ProjectRoot != "" {
		ps, err := DiscoverFromDir(ProjectFrameworksDir(opts.ProjectRoot))
		if err != nil {
			return nil, fmt.Errorf("discover project frameworks: %w", err)
		}
		for _, p := range ps {
			if err := r.Add(p, SourceProject); err != nil {
				return nil, err
			}
		}
	}

	// 2. User
	if opts.UserDir != "" {
		ps, err := DiscoverFromDir(opts.UserDir)
		if err != nil {
			return nil, fmt.Errorf("discover user frameworks: %w", err)
		}
		for _, p := range ps {
			if err := r.Add(p, SourceUserHome); err != nil {
				return nil, err
			}
		}
	}

	// 3. Embedded (in-tree)
	if opts.StageDir != "" {
		ps, err := LoadEmbedded(opts.StageDir)
		if err != nil {
			return nil, fmt.Errorf("load embedded frameworks: %w", err)
		}
		for _, p := range ps {
			if err := r.Add(p, SourceInTree); err != nil {
				return nil, err
			}
		}
	}

	return r, nil
}
