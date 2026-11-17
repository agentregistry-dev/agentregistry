package v1alpha1

// Repository is a source-code location shared by several resource kinds.
//
// Branch and Commit are optional pinning hints for consumers that need to
// fetch source for deployment or other runtime work. When both are empty,
// consumers should fall back to the repository's default branch (i.e.
// `git clone` without `--branch`), not a hardcoded branch name.
type Repository struct {
	URL       string `json:"url,omitempty" yaml:"url,omitempty"`
	Branch    string `json:"branch,omitempty" yaml:"branch,omitempty"`
	Commit    string `json:"commit,omitempty" yaml:"commit,omitempty"`
	Subfolder string `json:"subfolder,omitempty" yaml:"subfolder,omitempty"`

	// CredentialsRef names a Secret holding credentials for a private
	// repository. Git needs a pair, so resolvers read username/password.
	CredentialsRef *LocalSecretReference `json:"credentialsRef,omitempty" yaml:"credentialsRef,omitempty"`
}

// LocalSecretReference names a Secret in the referring resource's namespace.
// Unlike SecretKeyRef it indexes no key: the consumer owns the key convention.
type LocalSecretReference struct {
	Name string `json:"name" yaml:"name"`
}
