package v1alpha1

// Repository is a source-code location shared by several resource kinds.
//
// Branch and Commit are optional pinning hints for consumers that need to
// fetch source for deployment or other runtime work. When both are empty,
// consumers should fall back to the repository's default branch (i.e.
// `git clone` without `--branch`), not a hardcoded branch name.
type Repository struct {
	// url is part of Repository.
	// +optional
	URL string `json:"url,omitempty" yaml:"url,omitempty"`
	// branch is part of Repository.
	// +optional
	Branch string `json:"branch,omitempty" yaml:"branch,omitempty"`
	// commit is part of Repository.
	// +optional
	Commit string `json:"commit,omitempty" yaml:"commit,omitempty"`
	// subfolder is part of Repository.
	// +optional
	Subfolder string `json:"subfolder,omitempty" yaml:"subfolder,omitempty"`

	// credentialsRef names a Secret holding credentials for a private
	// repository. Git needs a pair, so resolvers read username/password.
	// +optional
	CredentialsRef *LocalSecretReference `json:"credentialsRef,omitempty" yaml:"credentialsRef,omitempty"`
}

// LocalSecretReference names a Secret in the referring resource's namespace.
// Unlike SecretKeyRef it indexes no key: the consumer owns the key convention.
type LocalSecretReference struct {
	// name is part of LocalSecretReference.
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name" yaml:"name"`
}
