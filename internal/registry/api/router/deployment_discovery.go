package router

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

const deploymentOriginAnnotation = "agentregistry.solo.io/origin"

type deploymentDiscoverer interface {
	Discover(ctx context.Context, runtime *v1alpha1.Runtime) ([]types.DiscoveryResult, error)
}

func deploymentDiscoveryListAugmenter(stores Stores, discoverer deploymentDiscoverer) func(context.Context, resource.ListAugmentInput) ([]*v1alpha1.RawObject, error) {
	return func(ctx context.Context, in resource.ListAugmentInput) ([]*v1alpha1.RawObject, error) {
		if discoverer == nil || in.Origin == "managed" || in.Cursor != "" || in.Labels != "" {
			return nil, nil
		}
		runtimeStore := stores[v1alpha1.KindRuntime]
		if runtimeStore == nil {
			return nil, nil
		}

		runtimes, _, err := runtimeStore.List(ctx, v1alpha1store.ListOpts{
			Namespace: in.Namespace,
			Limit:     1000,
		})
		if err != nil {
			return nil, fmt.Errorf("list runtimes for discovery: %w", err)
		}

		seenNames, seenManagedTargets := discoveredDeploymentSeenSets(in.Existing)
		out := make([]*v1alpha1.RawObject, 0)
		for _, rawRuntime := range runtimes {
			runtime, err := v1alpha1.EnvelopeFromRaw(func() *v1alpha1.Runtime { return &v1alpha1.Runtime{} }, rawRuntime, v1alpha1.KindRuntime)
			if err != nil {
				return nil, fmt.Errorf("decode runtime for discovery: %w", err)
			}
			results, err := discoverer.Discover(ctx, runtime)
			if err != nil {
				if deploymentsvc.IsUnsupportedDeploymentRuntimeError(err) {
					continue
				}
				return nil, fmt.Errorf("discover runtime %s/%s: %w", runtime.Metadata.NamespaceOrDefault(), runtime.Metadata.Name, err)
			}
			for _, result := range results {
				row, ok := discoveredDeploymentRaw(runtime, result, seenNames, seenManagedTargets)
				if !ok {
					continue
				}
				out = append(out, row)
			}
		}
		return out, nil
	}
}

func discoveredDeploymentSeenSets(existing []*v1alpha1.RawObject) (map[string]struct{}, map[string]struct{}) {
	names := map[string]struct{}{}
	targets := map[string]struct{}{}
	for _, raw := range existing {
		if raw == nil {
			continue
		}
		ns := raw.Metadata.NamespaceOrDefault()
		names[ns+"/"+raw.Metadata.Name] = struct{}{}

		var spec v1alpha1.DeploymentSpec
		if len(raw.Spec) == 0 || json.Unmarshal(raw.Spec, &spec) != nil {
			continue
		}
		targetNames := []string{spec.TargetRef.Name}
		targetNames = append(targetNames, managedDeploymentRemoteNames(raw.Metadata.Annotations)...)
		seenTargetNames := map[string]struct{}{}
		for _, targetName := range targetNames {
			targetName = strings.TrimSpace(targetName)
			if targetName == "" {
				continue
			}
			if _, ok := seenTargetNames[targetName]; ok {
				continue
			}
			seenTargetNames[targetName] = struct{}{}
			targets[managedDeploymentTargetKey(ns, spec.RuntimeRef.Name, spec.TargetRef.Kind, targetName)] = struct{}{}
		}
	}
	return names, targets
}

func managedDeploymentRemoteNames(annotations map[string]string) []string {
	if len(annotations) == 0 {
		return nil
	}
	names := make([]string, 0, 2)
	for key, value := range annotations {
		key = strings.TrimSpace(key)
		if strings.HasSuffix(key, "/remoteName") || strings.HasSuffix(key, "/remoteId") {
			if value = strings.TrimSpace(value); value != "" {
				names = append(names, value)
			}
		}
	}
	return names
}

func discoveredDeploymentRaw(
	runtime *v1alpha1.Runtime,
	result types.DiscoveryResult,
	seenNames map[string]struct{},
	seenManagedTargets map[string]struct{},
) (*v1alpha1.RawObject, bool) {
	if runtime == nil {
		return nil, false
	}
	targetKind := strings.TrimSpace(result.TargetKind)
	if targetKind != v1alpha1.KindAgent && targetKind != v1alpha1.KindMCPServer {
		return nil, false
	}
	targetName := discoveredTargetName(result)
	if targetName == "" {
		return nil, false
	}
	ns := strings.TrimSpace(result.Namespace)
	if ns == "" {
		ns = runtime.Metadata.NamespaceOrDefault()
	}
	runtimeName := strings.TrimSpace(runtime.Metadata.Name)
	if runtimeName == "" {
		return nil, false
	}
	if _, ok := seenManagedTargets[managedDeploymentTargetKey(ns, runtimeName, targetKind, targetName)]; ok {
		return nil, false
	}

	tag := strings.TrimSpace(result.Tag)
	if tag == "" {
		tag = "unknown"
	}
	name := discoveredDeploymentName(runtimeName, targetKind, targetName, tag, ns)
	if _, ok := seenNames[ns+"/"+name]; ok {
		return nil, false
	}
	seenNames[ns+"/"+name] = struct{}{}

	spec := v1alpha1.DeploymentSpec{
		TargetRef: v1alpha1.ResourceRef{
			Kind: targetKind,
			Name: targetName,
			Tag:  tag,
		},
		RuntimeRef: v1alpha1.ResourceRef{
			Kind: v1alpha1.KindRuntime,
			Name: runtimeName,
		},
	}
	status := v1alpha1.Status{}
	status.SetCondition(v1alpha1.Condition{
		Type:               "Ready",
		Status:             v1alpha1.ConditionTrue,
		Reason:             "Discovered",
		Message:            "discovered from runtime",
		LastTransitionTime: time.Now().UTC(),
	})
	if len(result.RuntimeMetadata) > 0 {
		_ = status.SetDetailsKey("runtimeMetadata", result.RuntimeMetadata)
	}

	specJSON, err := json.Marshal(spec)
	if err != nil {
		return nil, false
	}
	statusJSON, err := v1alpha1.MarshalStatusForStorage(status)
	if err != nil {
		return nil, false
	}
	return &v1alpha1.RawObject{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindDeployment},
		Metadata: v1alpha1.ObjectMeta{
			Namespace: ns,
			Name:      name,
			Annotations: map[string]string{
				deploymentOriginAnnotation: "discovered",
			},
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
		Spec:   specJSON,
		Status: statusJSON,
	}, true
}

func discoveredTargetName(result types.DiscoveryResult) string {
	if name := strings.TrimSpace(result.Name); name != "" {
		return name
	}
	for _, key := range []string{"remoteName", "remoteId"} {
		if value := strings.TrimSpace(result.RuntimeMetadata[key]); value != "" {
			return value
		}
	}
	return ""
}

func discoveredDeploymentName(runtimeName, targetKind, targetName, tag, namespace string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		namespace,
		runtimeName,
		targetKind,
		targetName,
		tag,
	}, "\x00")))
	return "discovered-" + hex.EncodeToString(sum[:])[:16]
}

func managedDeploymentTargetKey(namespace, runtimeName, targetKind, targetName string) string {
	return strings.Join([]string{
		strings.TrimSpace(namespace),
		strings.TrimSpace(runtimeName),
		strings.TrimSpace(targetKind),
		strings.TrimSpace(targetName),
	}, "\x00")
}
