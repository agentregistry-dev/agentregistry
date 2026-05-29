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

// DefaultApplyFingerprint returns a deterministic fingerprint of the resolved
// desired apply input. It intentionally excludes status, labels, annotations,
// finalizers, timestamps, and other controller bookkeeping.
func DefaultApplyFingerprint(ctx context.Context, in ApplyInput, opts ApplyFingerprintOptions) (string, error) {
	if in.Deployment == nil {
		return "", fmt.Errorf("fingerprint: deployment is required")
	}
	if in.Target == nil {
		return "", fmt.Errorf("fingerprint: target is required")
	}
	if in.Runtime == nil {
		return "", fmt.Errorf("fingerprint: runtime is required")
	}

	deps := append([]v1alpha1.Object(nil), opts.Dependencies...)
	defaultDeps, err := defaultApplyDependencies(ctx, in)
	if err != nil {
		return "", err
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
		return "", err
	}
	if payload.Target, err = objectFingerprint("", in.Target); err != nil {
		return "", err
	}
	if payload.Runtime, err = objectFingerprint(v1alpha1.KindRuntime, in.Runtime); err != nil {
		return "", err
	}
	for _, dep := range deps {
		fp, err := objectFingerprint("", dep)
		if err != nil {
			return "", err
		}
		payload.Dependencies = append(payload.Dependencies, fp)
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("fingerprint: marshal payload: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
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
	return fingerprintObject{
		Kind:       kind,
		Namespace:  meta.NamespaceOrDefault(),
		Name:       meta.Name,
		Tag:        meta.Tag,
		UID:        meta.UID,
		Generation: meta.Generation,
		Spec:       spec,
	}, nil
}

func defaultApplyDependencies(ctx context.Context, in ApplyInput) ([]v1alpha1.Object, error) {
	agent, ok := in.Target.(*v1alpha1.Agent)
	if !ok || agent == nil || len(agent.Spec.MCPServers) == 0 {
		return nil, nil
	}
	if in.Getter == nil {
		return nil, fmt.Errorf("fingerprint: getter required to resolve Agent MCPServer refs")
	}
	deps := make([]v1alpha1.Object, 0, len(agent.Spec.MCPServers))
	for i, ref := range agent.Spec.MCPServers {
		normalized := ref
		if normalized.Kind == "" {
			normalized.Kind = v1alpha1.KindMCPServer
		}
		if normalized.Namespace == "" {
			normalized.Namespace = agent.Metadata.NamespaceOrDefault()
		}
		obj, err := in.Getter(ctx, normalized)
		if err != nil {
			return nil, fmt.Errorf("fingerprint: resolve target spec.mcpServers[%d] %s/%s: %w", i, normalized.Namespace, normalized.Name, err)
		}
		deps = append(deps, obj)
	}
	return deps, nil
}
