package types

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// DeploymentDesiredFingerprinter lets an adapter define exactly which resolved
// inputs determine its external output. Adapters that do not implement this
// hook use DefaultApplyFingerprint.
type DeploymentDesiredFingerprinter interface {
	DesiredFingerprint(ctx context.Context, in ApplyInput) (string, error)
}

// ApplyFingerprintOptions carries adapter-owned inputs that are not already
// represented by ApplyInput. Dependencies are additional resolved resources
// the adapter will read while materializing the target.
type ApplyFingerprintOptions struct {
	AdapterType  string
	Dependencies []v1alpha1.Object
	Extra        any
}

// ApplyDependencySnapshot is the operator-visible identity of one resolved
// resource that influenced a Deployment apply fingerprint.
type ApplyDependencySnapshot struct {
	Kind         string `json:"kind"`
	Namespace    string `json:"namespace,omitempty"`
	Name         string `json:"name"`
	Tag          string `json:"tag,omitempty"`
	UID          string `json:"uid,omitempty"`
	Generation   int64  `json:"generation,omitempty"`
	MaterialHash string `json:"materialHash,omitempty"`
}

// ApplyFingerprintResult carries the fingerprint plus the resolved dependency
// evidence used to build it.
type ApplyFingerprintResult struct {
	Fingerprint  string
	Dependencies []ApplyDependencySnapshot
}

// DefaultApplyFingerprint returns a deterministic fingerprint of the resolved
// desired apply input. It intentionally excludes status, labels, annotations,
// finalizers, timestamps, and other controller bookkeeping. The fingerprint is
// based on declared intent, not remote drift; for example, a Deployment that
// names a git branch without a commit SHA keeps the same fingerprint as that
// branch's HEAD moves, and operators should change the spec or use the
// controller force token when they want a rebuild from the new HEAD.
func DefaultApplyFingerprint(ctx context.Context, in ApplyInput, opts ApplyFingerprintOptions) (string, error) {
	result, err := DefaultApplyFingerprintResult(ctx, in, opts)
	if err != nil {
		return "", err
	}
	return result.Fingerprint, nil
}

// DefaultApplyFingerprintResult returns the same deterministic fingerprint as
// DefaultApplyFingerprint, plus snapshots of resolved dependency resources
// that participated in the fingerprint payload.
func DefaultApplyFingerprintResult(ctx context.Context, in ApplyInput, opts ApplyFingerprintOptions) (ApplyFingerprintResult, error) {
	if in.Deployment == nil {
		return ApplyFingerprintResult{}, fmt.Errorf("fingerprint: deployment is required")
	}
	if in.Target == nil {
		return ApplyFingerprintResult{}, fmt.Errorf("fingerprint: target is required")
	}
	if in.Runtime == nil {
		return ApplyFingerprintResult{}, fmt.Errorf("fingerprint: runtime is required")
	}

	deps := append([]v1alpha1.Object(nil), opts.Dependencies...)
	defaultDeps, err := defaultApplyDependencies(ctx, in)
	if err != nil {
		return ApplyFingerprintResult{}, err
	}
	deps = append(deps, defaultDeps...)

	payload := applyFingerprintPayload{
		Version:      1,
		AdapterType:  opts.AdapterType,
		Deployment:   fingerprintObject{},
		Target:       fingerprintObject{},
		Runtime:      fingerprintObject{},
		Dependencies: make([]fingerprintObject, 0, len(deps)),
		Extra:        opts.Extra,
	}
	if payload.Deployment, err = objectFingerprint(v1alpha1.KindDeployment, in.Deployment); err != nil {
		return ApplyFingerprintResult{}, err
	}
	if payload.Target, err = objectFingerprint("", in.Target); err != nil {
		return ApplyFingerprintResult{}, err
	}
	if payload.Runtime, err = objectFingerprint(v1alpha1.KindRuntime, in.Runtime); err != nil {
		return ApplyFingerprintResult{}, err
	}
	snapshots := make([]ApplyDependencySnapshot, 0, len(deps))
	for _, dep := range deps {
		fp, err := objectFingerprint("", dep)
		if err != nil {
			return ApplyFingerprintResult{}, err
		}
		payload.Dependencies = append(payload.Dependencies, fp)
		snapshots = append(snapshots, dependencySnapshot(fp))
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return ApplyFingerprintResult{}, fmt.Errorf("fingerprint: marshal payload: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return ApplyFingerprintResult{
		Fingerprint:  "sha256:" + hex.EncodeToString(sum[:]),
		Dependencies: snapshots,
	}, nil
}

type applyFingerprintPayload struct {
	Version      int                 `json:"version"`
	AdapterType  string              `json:"adapterType,omitempty"`
	Deployment   fingerprintObject   `json:"deployment"`
	Target       fingerprintObject   `json:"target"`
	Runtime      fingerprintObject   `json:"runtime"`
	Dependencies []fingerprintObject `json:"dependencies,omitempty"`
	Extra        any                 `json:"extra,omitempty"`
}

type fingerprintObject struct {
	Kind       string          `json:"kind"`
	Namespace  string          `json:"namespace,omitempty"`
	Name       string          `json:"name"`
	Tag        string          `json:"tag,omitempty"`
	UID        string          `json:"uid,omitempty"`
	Generation int64           `json:"generation,omitempty"`
	Spec       json.RawMessage `json:"spec,omitempty"`
	Material   json.RawMessage `json:"material,omitempty"`
}

func objectFingerprint(defaultKind string, obj v1alpha1.Object) (fingerprintObject, error) {
	if obj == nil {
		return fingerprintObject{}, fmt.Errorf("fingerprint: nil object")
	}
	meta := obj.GetMetadata()
	if meta == nil {
		return fingerprintObject{}, fmt.Errorf("fingerprint: %s metadata is required", obj.GetKind())
	}
	kind := obj.GetKind()
	if kind == "" {
		kind = defaultKind
	}
	if kind == "" {
		return fingerprintObject{}, fmt.Errorf("fingerprint: kind is required for %s/%s", meta.NamespaceOrDefault(), meta.Name)
	}
	spec, err := obj.MarshalSpec()
	if err != nil {
		return fingerprintObject{}, fmt.Errorf("fingerprint: marshal %s %s/%s spec: %w", kind, meta.NamespaceOrDefault(), meta.Name, err)
	}
	material, err := dependencyMaterial(kind, obj)
	if err != nil {
		return fingerprintObject{}, err
	}
	return fingerprintObject{
		Kind:       kind,
		Namespace:  meta.NamespaceOrDefault(),
		Name:       meta.Name,
		Tag:        meta.Tag,
		UID:        meta.UID,
		Generation: meta.Generation,
		Spec:       spec,
		Material:   material,
	}, nil
}

func dependencySnapshot(fp fingerprintObject) ApplyDependencySnapshot {
	return ApplyDependencySnapshot{
		Kind:         fp.Kind,
		Namespace:    fp.Namespace,
		Name:         fp.Name,
		Tag:          fp.Tag,
		UID:          fp.UID,
		Generation:   fp.Generation,
		MaterialHash: materialHash(fp.Spec, fp.Material),
	}
}

func dependencyMaterial(kind string, obj v1alpha1.Object) (json.RawMessage, error) {
	switch kind {
	case v1alpha1.KindPlugin:
		plugin, ok := obj.(*v1alpha1.Plugin)
		if !ok || plugin.Status.ResolvedSource == nil {
			return nil, nil
		}
		return json.Marshal(struct {
			ResolvedSource *v1alpha1.PluginResolvedSource `json:"resolvedSource"`
		}{ResolvedSource: plugin.Status.ResolvedSource})
	case v1alpha1.KindSkill:
		skill, ok := obj.(*v1alpha1.Skill)
		if !ok || skill.Status.ResolvedSource == nil {
			return nil, nil
		}
		return json.Marshal(struct {
			ResolvedSource *v1alpha1.SkillResolvedSource `json:"resolvedSource"`
		}{ResolvedSource: skill.Status.ResolvedSource})
	default:
		return nil, nil
	}
}

func materialHash(parts ...json.RawMessage) string {
	hasMaterial := false
	for _, part := range parts {
		if len(part) > 0 {
			hasMaterial = true
			break
		}
	}
	if !hasMaterial {
		return ""
	}
	h := sha256.New()
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		h.Write([]byte{0})
		h.Write(part)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func defaultApplyDependencies(ctx context.Context, in ApplyInput) ([]v1alpha1.Object, error) {
	agent, ok := in.Target.(*v1alpha1.Agent)
	if !ok || agent == nil {
		return nil, nil
	}
	if in.Getter == nil {
		if len(agent.Spec.MCPServers) > 0 || hasHarnessCompositionRefs(agent) {
			return nil, fmt.Errorf("fingerprint: getter required to resolve Agent dependency refs")
		}
		return nil, nil
	}
	deps := make([]v1alpha1.Object, 0, len(agent.Spec.MCPServers)+len(agent.Spec.Plugins)+len(agent.Spec.Skills)+1)
	var err error
	deps, err = appendResolvedRefs(ctx, deps, in.Getter, agent.Metadata.NamespaceOrDefault(), agent.Spec.MCPServers, v1alpha1.KindMCPServer, "target spec.mcpServers")
	if err != nil {
		return nil, err
	}
	if agent.Spec.Source == nil || agent.Spec.Source.Harness == nil {
		return deps, nil
	}
	deps, err = appendResolvedRefs(ctx, deps, in.Getter, agent.Metadata.NamespaceOrDefault(), agent.Spec.Plugins, v1alpha1.KindPlugin, "target spec.plugins")
	if err != nil {
		return nil, err
	}
	deps, err = appendResolvedRefs(ctx, deps, in.Getter, agent.Metadata.NamespaceOrDefault(), agent.Spec.Skills, v1alpha1.KindSkill, "target spec.skills")
	if err != nil {
		return nil, err
	}
	if agent.Spec.Instructions != nil {
		deps, err = appendResolvedRefs(ctx, deps, in.Getter, agent.Metadata.NamespaceOrDefault(), []v1alpha1.ResourceRef{*agent.Spec.Instructions}, v1alpha1.KindPrompt, "target spec.instructions")
		if err != nil {
			return nil, err
		}
	}
	return deps, nil
}

func hasHarnessCompositionRefs(agent *v1alpha1.Agent) bool {
	return agent != nil && agent.Spec.Source != nil && agent.Spec.Source.Harness != nil &&
		(len(agent.Spec.Plugins) > 0 || len(agent.Spec.Skills) > 0 || agent.Spec.Instructions != nil)
}

func appendResolvedRefs(
	ctx context.Context,
	deps []v1alpha1.Object,
	getter v1alpha1.GetterFunc,
	namespace string,
	refs []v1alpha1.ResourceRef,
	defaultKind string,
	field string,
) ([]v1alpha1.Object, error) {
	for i, ref := range refs {
		normalized := ref
		if normalized.Kind == "" {
			normalized.Kind = defaultKind
		}
		if normalized.Namespace == "" {
			normalized.Namespace = namespace
		}
		obj, err := getter(ctx, normalized)
		if err != nil {
			return nil, fmt.Errorf("fingerprint: resolve %s[%d] %s/%s: %w", field, i, normalized.Namespace, normalized.Name, err)
		}
		deps = append(deps, obj)
	}
	return deps, nil
}
