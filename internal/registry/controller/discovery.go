package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

const (
	deploymentDiscoveryCondition = "Discovered"
	deploymentRuntimeDetailsKey  = "runtimeMetadata"

	// deploymentDiscoveryMissesKey tracks, in status details, how many
	// consecutive successful polls omitted a discovered row. Provider list
	// APIs (notably AWS) are eventually consistent, so a single missing poll
	// is not evidence the workload is gone.
	deploymentDiscoveryMissesKey = "discoveryMisses"
	// deploymentDiscoveryStaleAfterMisses is how many consecutive misses it
	// takes before the Discovered/Ready conditions flip to False.
	deploymentDiscoveryStaleAfterMisses = 3
	// deploymentDiscoveryDeleteAfterMisses is how many consecutive misses it
	// takes before the row is deleted outright. Rows whose Runtime no longer
	// exists skip the counter and are deleted immediately — no future poll
	// could confirm them again.
	deploymentDiscoveryDeleteAfterMisses = 5
)

// DeploymentDiscoveryController materializes adapter discovery snapshots into
// persisted Deployment rows. It owns provider-observed state; the normal
// DeploymentController skips these rows so they do not become desired state.
type DeploymentDiscoveryController struct {
	Stores   map[string]*v1alpha1store.Store
	Adapters map[string]types.DeploymentAdapter
}

// DeploymentDiscoverySyncResult summarizes one discovery materialization pass.
type DeploymentDiscoverySyncResult struct {
	Runtimes   int
	Discovered int
	// Stale counts discovered rows currently at or past the consecutive-miss
	// staleness threshold (their Discovered/Ready conditions are False).
	Stale int
	// Removed counts discovered rows deleted this pass — either past the
	// consecutive-miss deletion threshold or orphaned by Runtime deletion.
	Removed int
}

func (c *DeploymentDiscoveryController) Run(ctx context.Context, interval time.Duration) error {
	if c == nil {
		return fmt.Errorf("deployment discovery controller: controller is required")
	}
	if interval <= 0 {
		interval = defaultControllerResyncInterval
	}
	for {
		result, err := c.Sync(ctx)
		if err != nil {
			logger.Error("deployment discovery sync failed", "error", err)
		} else {
			logger.Debug("deployment discovery synced", "runtimes", result.Runtimes, "discovered", result.Discovered, "stale", result.Stale, "removed", result.Removed)
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *DeploymentDiscoveryController) Sync(ctx context.Context) (DeploymentDiscoverySyncResult, error) {
	runtimeStore := c.runtimeStore()
	deploymentStore := c.deploymentStore()
	if runtimeStore == nil || deploymentStore == nil {
		return DeploymentDiscoverySyncResult{}, fmt.Errorf("deployment discovery controller: Runtime and Deployment stores are required")
	}
	runtimes, err := c.listRuntimes(ctx)
	if err != nil {
		return DeploymentDiscoverySyncResult{}, err
	}
	index, err := c.loadDiscoveryIndex(ctx)
	if err != nil {
		return DeploymentDiscoverySyncResult{}, err
	}

	var result DeploymentDiscoverySyncResult
	var firstErr error
	observed := map[string]struct{}{}
	knownRuntimes := map[string]struct{}{}
	successfulRuntimes := map[string]struct{}{}
	for _, runtime := range runtimes {
		if runtime == nil {
			continue
		}
		knownRuntimes[runtimeDiscoveryKey(runtime.Metadata.NamespaceOrDefault(), runtime.Metadata.Name)] = struct{}{}
		adapter := c.Adapters[strings.TrimSpace(runtime.Spec.Type)]
		if adapter == nil {
			continue
		}
		source, ok := adapter.(types.DeploymentDiscoverySource)
		if !ok {
			continue
		}
		discovered, err := source.Discover(ctx, types.DiscoverInput{Runtime: runtime})
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("discover runtime %s/%s: %w", runtime.Metadata.NamespaceOrDefault(), runtime.Metadata.Name, err)
			}
			continue
		}
		result.Runtimes++
		successfulRuntimes[runtimeDiscoveryKey(runtime.Metadata.NamespaceOrDefault(), runtime.Metadata.Name)] = struct{}{}
		for _, discovery := range discovered {
			deployment, ok := discoveredDeploymentFromResult(runtime, discovery, index)
			if !ok {
				continue
			}
			key := deploymentKey(deployment.Metadata.NamespaceOrDefault(), deployment.Metadata.Name)
			if _, ok := observed[key]; ok {
				continue
			}
			upserted, err := deploymentStore.Upsert(ctx, deployment)
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("upsert discovered Deployment %s: %w", key, err)
				}
				continue
			}
			if err := c.patchDiscoveredStatus(ctx, deployment, upserted.Generation, discovery.RuntimeMetadata); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			observed[key] = struct{}{}
			result.Discovered++
		}
	}

	for key, deployment := range index.discovered {
		if _, ok := observed[key]; ok {
			continue
		}
		runtimeKey := deploymentRuntimeKey(deployment)
		if _, ok := knownRuntimes[runtimeKey]; !ok {
			// The owning Runtime row is gone, so no future poll can confirm
			// this workload again. Remove the row immediately.
			if err := c.deleteDiscoveredDeployment(ctx, deployment); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			result.Removed++
			continue
		}
		if _, ok := successfulRuntimes[runtimeKey]; !ok {
			// The Runtime exists but this pass could not confirm provider
			// state (discovery errored, or the adapter does not implement
			// discovery). Leave the row untouched rather than guess.
			continue
		}
		misses := discoveredMissCount(deployment) + 1
		if misses >= deploymentDiscoveryDeleteAfterMisses {
			if err := c.deleteDiscoveredDeployment(ctx, deployment); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			result.Removed++
			continue
		}
		if err := c.patchMissedStatus(ctx, deployment, misses); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if misses >= deploymentDiscoveryStaleAfterMisses {
			result.Stale++
		}
	}
	return result, firstErr
}

type deploymentDiscoveryIndex struct {
	discovered     map[string]*v1alpha1.Deployment
	managedNames   map[string]struct{}
	managedTargets map[string]struct{}
}

func (c *DeploymentDiscoveryController) listRuntimes(ctx context.Context) ([]*v1alpha1.Runtime, error) {
	store := c.runtimeStore()
	if store == nil {
		return nil, fmt.Errorf("deployment discovery controller: no Runtime store registered")
	}
	var out []*v1alpha1.Runtime
	opts := v1alpha1store.ListOpts{Limit: defaultControllerListPageSize}
	for {
		rows, cursor, err := store.List(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("deployment discovery controller: list Runtimes: %w", err)
		}
		for _, raw := range rows {
			runtime, err := v1alpha1.EnvelopeFromRaw(func() *v1alpha1.Runtime {
				return &v1alpha1.Runtime{}
			}, raw, v1alpha1.KindRuntime)
			if err != nil {
				return nil, fmt.Errorf("deployment discovery controller: decode Runtime: %w", err)
			}
			out = append(out, runtime)
		}
		if cursor == "" {
			return out, nil
		}
		opts.Cursor = cursor
	}
}

func (c *DeploymentDiscoveryController) loadDiscoveryIndex(ctx context.Context) (deploymentDiscoveryIndex, error) {
	store := c.deploymentStore()
	if store == nil {
		return deploymentDiscoveryIndex{}, fmt.Errorf("deployment discovery controller: no Deployment store registered")
	}
	index := deploymentDiscoveryIndex{
		discovered:     map[string]*v1alpha1.Deployment{},
		managedNames:   map[string]struct{}{},
		managedTargets: map[string]struct{}{},
	}
	opts := v1alpha1store.ListOpts{Limit: defaultControllerListPageSize}
	for {
		rows, cursor, err := store.List(ctx, opts)
		if err != nil {
			return deploymentDiscoveryIndex{}, fmt.Errorf("deployment discovery controller: list Deployments: %w", err)
		}
		for _, raw := range rows {
			deployment, err := v1alpha1.EnvelopeFromRaw(func() *v1alpha1.Deployment {
				return &v1alpha1.Deployment{}
			}, raw, v1alpha1.KindDeployment)
			if err != nil {
				return deploymentDiscoveryIndex{}, fmt.Errorf("deployment discovery controller: decode Deployment: %w", err)
			}
			key := deploymentKey(deployment.Metadata.NamespaceOrDefault(), deployment.Metadata.Name)
			if v1alpha1.IsDiscoveredDeployment(deployment) {
				index.discovered[key] = deployment
				continue
			}
			index.managedNames[key] = struct{}{}
			for _, targetName := range managedDeploymentTargetNames(deployment) {
				index.managedTargets[managedDeploymentTargetKey(
					deployment.Metadata.NamespaceOrDefault(),
					deployment.Spec.RuntimeRef.Name,
					deployment.Spec.TargetRef.Kind,
					targetName,
				)] = struct{}{}
			}
		}
		if cursor == "" {
			return index, nil
		}
		opts.Cursor = cursor
	}
}

func discoveredDeploymentFromResult(
	runtime *v1alpha1.Runtime,
	result types.DiscoveryResult,
	index deploymentDiscoveryIndex,
) (*v1alpha1.Deployment, bool) {
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
	tag := strings.TrimSpace(result.Tag)
	if tag == "" {
		tag = "unknown"
	}
	if _, ok := index.managedTargets[managedDeploymentTargetKey(ns, runtimeName, targetKind, targetName)]; ok {
		return nil, false
	}

	name := discoveredDeploymentName(runtimeName, targetKind, targetName, tag, ns)
	if _, ok := index.managedNames[deploymentKey(ns, name)]; ok {
		return nil, false
	}

	runtimeRef := v1alpha1.ResourceRef{
		Kind: v1alpha1.KindRuntime,
		Name: runtimeName,
	}
	runtimeNS := runtime.Metadata.NamespaceOrDefault()
	if runtimeNS != ns {
		runtimeRef.Namespace = runtimeNS
	}
	return &v1alpha1.Deployment{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindDeployment},
		Metadata: v1alpha1.ObjectMeta{
			Namespace: ns,
			Name:      name,
			Annotations: map[string]string{
				v1alpha1.DeploymentOriginAnnotation:                v1alpha1.DeploymentOriginDiscovered,
				v1alpha1.DeploymentDiscoveredRuntimeAnnotation:     runtimeName,
				v1alpha1.DeploymentDiscoveredRuntimeTypeAnnotation: runtime.Spec.Type,
			},
		},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef: v1alpha1.ResourceRef{
				Kind: targetKind,
				Name: targetName,
				Tag:  tag,
			},
			RuntimeRef:   runtimeRef,
			DesiredState: v1alpha1.DesiredStateDeployed,
		},
	}, true
}

func (c *DeploymentDiscoveryController) patchDiscoveredStatus(
	ctx context.Context,
	deployment *v1alpha1.Deployment,
	generation int64,
	runtimeMetadata map[string]string,
) error {
	now := time.Now().UTC()
	err := c.deploymentStore().PatchStatus(ctx, deployment.Metadata.NamespaceOrDefault(), deployment.Metadata.Name, "", v1alpha1.StatusPatcher(func(s *v1alpha1.Status) {
		if s.ObservedGeneration < generation {
			s.ObservedGeneration = generation
		}
		s.SetCondition(v1alpha1.Condition{
			Type:               "Ready",
			Status:             v1alpha1.ConditionTrue,
			Reason:             "Discovered",
			Message:            "discovered from runtime",
			LastTransitionTime: now,
			ObservedGeneration: generation,
		})
		s.SetCondition(v1alpha1.Condition{
			Type:               deploymentDiscoveryCondition,
			Status:             v1alpha1.ConditionTrue,
			Reason:             "ProviderObserved",
			Message:            "returned by latest discovery poll",
			LastTransitionTime: now,
			ObservedGeneration: generation,
		})
		if len(runtimeMetadata) > 0 {
			_ = s.SetDetailsKey(deploymentRuntimeDetailsKey, runtimeMetadata)
		} else {
			_ = s.SetDetailsKey(deploymentRuntimeDetailsKey, nil)
		}
		_ = s.SetDetailsKey(deploymentDiscoveryMissesKey, nil)
	}))
	if err != nil {
		return fmt.Errorf("patch discovered Deployment status %s/%s: %w", deployment.Metadata.NamespaceOrDefault(), deployment.Metadata.Name, err)
	}
	return nil
}

// patchMissedStatus records one more consecutive miss for a discovered row.
// Below the staleness threshold only the counter changes; at or past it the
// Discovered and Ready conditions flip to False so list consumers can see the
// workload is no longer observed on the provider.
func (c *DeploymentDiscoveryController) patchMissedStatus(ctx context.Context, deployment *v1alpha1.Deployment, misses int) error {
	now := time.Now().UTC()
	generation := deployment.Metadata.Generation
	err := c.deploymentStore().PatchStatus(ctx, deployment.Metadata.NamespaceOrDefault(), deployment.Metadata.Name, "", v1alpha1.StatusPatcher(func(s *v1alpha1.Status) {
		if s.ObservedGeneration < generation {
			s.ObservedGeneration = generation
		}
		_ = s.SetDetailsKey(deploymentDiscoveryMissesKey, misses)
		if misses < deploymentDiscoveryStaleAfterMisses {
			return
		}
		s.SetCondition(v1alpha1.Condition{
			Type:               deploymentDiscoveryCondition,
			Status:             v1alpha1.ConditionFalse,
			Reason:             "ProviderMissing",
			Message:            "not returned by recent discovery polls",
			LastTransitionTime: now,
			ObservedGeneration: generation,
		})
		s.SetCondition(v1alpha1.Condition{
			Type:               "Ready",
			Status:             v1alpha1.ConditionFalse,
			Reason:             "ProviderMissing",
			Message:            "no longer observed on the runtime",
			LastTransitionTime: now,
			ObservedGeneration: generation,
		})
	}))
	if err != nil {
		return fmt.Errorf("patch missed discovered Deployment status %s/%s: %w", deployment.Metadata.NamespaceOrDefault(), deployment.Metadata.Name, err)
	}
	return nil
}

// deleteDiscoveredDeployment hard-deletes a discovered row. Discovered rows
// never carry the controller finalizer, so Delete removes them in one step; a
// concurrent deletion is treated as success.
func (c *DeploymentDiscoveryController) deleteDiscoveredDeployment(ctx context.Context, deployment *v1alpha1.Deployment) error {
	namespace := deployment.Metadata.NamespaceOrDefault()
	name := deployment.Metadata.Name
	if err := c.deploymentStore().Delete(ctx, namespace, name, ""); err != nil && !errors.Is(err, pkgdb.ErrNotFound) {
		return fmt.Errorf("delete discovered Deployment %s/%s: %w", namespace, name, err)
	}
	return nil
}

// discoveredMissCount reads the consecutive-miss counter persisted in status
// details. Absent or malformed values count as zero misses.
func discoveredMissCount(deployment *v1alpha1.Deployment) int {
	var misses int
	ok, err := deployment.Status.GetDetailsKey(deploymentDiscoveryMissesKey, &misses)
	if err != nil || !ok || misses < 0 {
		return 0
	}
	return misses
}

func (c *DeploymentDiscoveryController) runtimeStore() *v1alpha1store.Store {
	if c == nil || c.Stores == nil {
		return nil
	}
	return c.Stores[v1alpha1.KindRuntime]
}

func (c *DeploymentDiscoveryController) deploymentStore() *v1alpha1store.Store {
	if c == nil || c.Stores == nil {
		return nil
	}
	return c.Stores[v1alpha1.KindDeployment]
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

func managedDeploymentTargetNames(deployment *v1alpha1.Deployment) []string {
	if deployment == nil {
		return nil
	}
	names := []string{deployment.Spec.TargetRef.Name}
	for key, value := range deployment.Metadata.Annotations {
		key = strings.TrimSpace(key)
		if strings.HasSuffix(key, "/remoteName") || strings.HasSuffix(key, "/remoteId") {
			if value = strings.TrimSpace(value); value != "" {
				names = append(names, value)
			}
		}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func discoveredDeploymentName(runtimeName, targetKind, targetName, tag, namespace string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		namespace,
		runtimeName,
		targetKind,
		targetName,
		tag,
	}, "\x00")))
	suffix := hex.EncodeToString(sum[:])[:12]
	prefix := "discovered-"
	readable := sanitizeDiscoveryName(targetName)
	maxReadable := 63 - len(prefix) - 1 - len(suffix)
	if len(readable) > maxReadable {
		readable = strings.Trim(readable[:maxReadable], "-")
	}
	if readable == "" {
		readable = "workload"
	}
	return prefix + readable + "-" + suffix
}

func sanitizeDiscoveryName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastHyphen := false
	for _, r := range value {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-'
		if !valid {
			r = '-'
		}
		if r == '-' {
			if lastHyphen {
				continue
			}
			lastHyphen = true
		} else {
			lastHyphen = false
		}
		b.WriteRune(r)
	}
	return strings.Trim(b.String(), "-")
}

func managedDeploymentTargetKey(namespace, runtimeName, targetKind, targetName string) string {
	return strings.Join([]string{
		strings.TrimSpace(namespace),
		strings.TrimSpace(runtimeName),
		strings.TrimSpace(targetKind),
		strings.TrimSpace(targetName),
	}, "\x00")
}

func deploymentRuntimeKey(deployment *v1alpha1.Deployment) string {
	if deployment == nil {
		return ""
	}
	namespace := deployment.Spec.RuntimeRef.Namespace
	if namespace == "" {
		namespace = deployment.Metadata.NamespaceOrDefault()
	}
	return runtimeDiscoveryKey(namespace, deployment.Spec.RuntimeRef.Name)
}

func runtimeDiscoveryKey(namespace, name string) string {
	return strings.Join([]string{strings.TrimSpace(namespace), strings.TrimSpace(name)}, "\x00")
}

func deploymentKey(namespace, name string) string {
	return strings.TrimSpace(namespace) + "/" + strings.TrimSpace(name)
}
