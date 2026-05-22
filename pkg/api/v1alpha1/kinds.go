package v1alpha1

import (
	"slices"
	"strings"
)

// KindStorage describes the private persistence semantics attached to a
// v1alpha1 kind. It lives with the API kind descriptor so storage, routing,
// and controller projection can share the same built-in kind metadata without
// each package maintaining its own kind switch.
type KindStorage string

const (
	KindStorageTaggedArtifact KindStorage = "TaggedArtifact"
	KindStorageMutableObject  KindStorage = "MutableObject"
)

// ProjectionPolicy captures controller read-model behavior that differs by
// kind. Derivers/executors remain feature-specific; this policy only tells the
// generic source projector how to read canonical rows.
type ProjectionPolicy struct {
	IncludeTerminating bool
}

// KindDescriptor is the single built-in registration record for a v1alpha1
// kind. Extension kinds can still use Scheme.Register and AppOptions store
// maps; built-ins should add one descriptor here instead of updating separate
// scheme/store/controller lists.
type KindDescriptor struct {
	Kind       string
	SpecSample any
	NewObject  func() any
	Storage    KindStorage
	Projection ProjectionPolicy
}

var builtinKindDescriptors = []KindDescriptor{
	{
		Kind:       KindAgent,
		SpecSample: AgentSpec{},
		NewObject:  func() any { return &Agent{} },
		Storage:    KindStorageTaggedArtifact,
	},
	{
		Kind:       KindMCPServer,
		SpecSample: MCPServerSpec{},
		NewObject:  func() any { return &MCPServer{} },
		Storage:    KindStorageTaggedArtifact,
	},
	{
		Kind:       KindSkill,
		SpecSample: SkillSpec{},
		NewObject:  func() any { return &Skill{} },
		Storage:    KindStorageTaggedArtifact,
	},
	{
		Kind:       KindPrompt,
		SpecSample: PromptSpec{},
		NewObject:  func() any { return &Prompt{} },
		Storage:    KindStorageTaggedArtifact,
	},
	{
		Kind:       KindRuntime,
		SpecSample: RuntimeSpec{},
		NewObject:  func() any { return &Runtime{} },
		Storage:    KindStorageMutableObject,
	},
	{
		Kind:       KindDeployment,
		SpecSample: DeploymentSpec{},
		NewObject:  func() any { return &Deployment{} },
		Storage:    KindStorageMutableObject,
		Projection: ProjectionPolicy{IncludeTerminating: true},
	},
}

// BuiltinKinds is the stable ordered list of Kind names this package defines.
// Iteration order is deterministic; callers building parallel structures
// should range over this slice or BuiltinKindDescriptors so they stay aligned
// as kinds are added. Extension kinds registered via Scheme.Register are NOT
// included here — those consumers bring their own list.
var BuiltinKinds = builtinKindNames(builtinKindDescriptors)

// BuiltinKindDescriptors returns the built-in descriptors in stable order.
func BuiltinKindDescriptors() []KindDescriptor {
	return slices.Clone(builtinKindDescriptors)
}

// BuiltinKindDescriptor returns the built-in descriptor for kind.
func BuiltinKindDescriptor(kind string) (KindDescriptor, bool) {
	for _, descriptor := range builtinKindDescriptors {
		if strings.EqualFold(descriptor.Kind, kind) {
			return descriptor, true
		}
	}
	return KindDescriptor{}, false
}

func builtinKindNames(descriptors []KindDescriptor) []string {
	kinds := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		kinds = append(kinds, descriptor.Kind)
	}
	return kinds
}
