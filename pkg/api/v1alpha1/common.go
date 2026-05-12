package v1alpha1

// Repository is a source-code location shared by several resource kinds.
//
// Branch and Commit are optional pinning hints for consumers that need to
// fetch source (deploys, importers, scanners). When both are empty,
// consumers should fall back to the repository's default branch (i.e.
// `git clone` without `--branch`), not a hardcoded branch name.
type Repository struct {
	URL       string `json:"url,omitempty" yaml:"url,omitempty"`
	Branch    string `json:"branch,omitempty" yaml:"branch,omitempty"`
	Commit    string `json:"commit,omitempty" yaml:"commit,omitempty"`
	Subfolder string `json:"subfolder,omitempty" yaml:"subfolder,omitempty"`
}
