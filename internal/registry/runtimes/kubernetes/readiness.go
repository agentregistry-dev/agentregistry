package kubernetes

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
	kmcpv1alpha1 "github.com/kagent-dev/kmcp/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// Reasons written on the Ready condition by ProbeReadiness.
const (
	readinessReasonAvailable        = "WorkloadAvailable"
	readinessReasonWaiting          = "WaitingForWorkload"
	readinessReasonRolloutStalled   = "RolloutStalled"
	readinessReasonNoWorkload       = "WorkloadNotFound"
	readinessReasonRemoteMCPApplied = "RemoteMCPServerApplied"
)

// workloadOwner is a kagent/kmcp resource that a Kubernetes Deployment is
// expected to be controller-owned by.
type workloadOwner struct {
	kind string
	name string
	uid  k8stypes.UID
}

// ProbeReadiness reports Ready=True once every kagent Agent and kmcp
// MCPServer labelled for this Deployment has a controller-owned apps/v1
// Deployment that Kubernetes reports as Available with its rollout complete.
// RemoteMCPServer-only deployments have no in-cluster workload and are ready
// as soon as the resource exists.
func (a *kubernetesDeploymentAdapter) ProbeReadiness(ctx context.Context, in types.ApplyInput) (*types.ReadinessResult, error) {
	if in.Deployment == nil {
		return nil, fmt.Errorf("probe readiness: deployment is required")
	}
	c, err := kubernetesGetClient(in.Runtime)
	if err != nil {
		return nil, err
	}
	namespace := namespaceFromV1Alpha1(in.Deployment, in.Runtime)
	deploymentID := in.Deployment.Metadata.Name
	gen := in.Deployment.Metadata.Generation
	now := time.Now().UTC()

	owners, remoteCount, err := kubernetesListWorkloadOwners(ctx, c, deploymentID, namespace)
	if err != nil {
		return nil, err
	}
	if len(owners) == 0 {
		if remoteCount > 0 {
			return readyResult(v1alpha1.ConditionTrue, readinessReasonRemoteMCPApplied, "remote MCP server configured", gen, now), nil
		}
		return readyResult(v1alpha1.ConditionUnknown, readinessReasonNoWorkload,
			fmt.Sprintf("no kagent resources found for deployment %q in namespace %q", deploymentID, namespace), gen, now), nil
	}

	workloads := &appsv1.DeploymentList{}
	if err := c.List(ctx, workloads, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("list deployments in namespace %s: %w", namespace, err)
	}
	byOwner := indexWorkloadsByOwner(workloads.Items)

	var pending []string
	stalled := false
	for _, owner := range owners {
		workload, ok := byOwner[owner.uid]
		if !ok {
			pending = append(pending, fmt.Sprintf("%s %s: workload not created yet", owner.kind, owner.name))
			continue
		}
		available, isStalled, detail := kubernetesWorkloadAvailable(workload)
		if isStalled {
			stalled = true
		}
		if !available {
			pending = append(pending, fmt.Sprintf("%s %s: %s", owner.kind, owner.name, detail))
		}
	}
	if len(pending) > 0 {
		if stalled {
			return readyResult(v1alpha1.ConditionFalse, readinessReasonRolloutStalled, strings.Join(pending, "; "), gen, now), nil
		}
		return readyResult(v1alpha1.ConditionUnknown, readinessReasonWaiting, strings.Join(pending, "; "), gen, now), nil
	}
	return readyResult(v1alpha1.ConditionTrue, readinessReasonAvailable, "Kubernetes reports all workloads available", gen, now), nil
}

func kubernetesListWorkloadOwners(
	ctx context.Context,
	c client.Client,
	deploymentID, namespace string,
) ([]workloadOwner, int, error) {
	opts := kubernetesDeploymentSelectorOpts(deploymentID, namespace)
	var owners []workloadOwner

	agents := &v1alpha2.AgentList{}
	if err := c.List(ctx, agents, opts...); err != nil {
		return nil, 0, fmt.Errorf("list agents by deployment id %s: %w", deploymentID, err)
	}
	for _, item := range agents.Items {
		if item.DeletionTimestamp == nil {
			owners = append(owners, workloadOwner{kind: "Agent", name: item.Name, uid: item.UID})
		}
	}

	servers := &kmcpv1alpha1.MCPServerList{}
	if err := c.List(ctx, servers, opts...); err != nil {
		return nil, 0, fmt.Errorf("list mcp servers by deployment id %s: %w", deploymentID, err)
	}
	for _, item := range servers.Items {
		if item.DeletionTimestamp == nil {
			owners = append(owners, workloadOwner{kind: "MCPServer", name: item.Name, uid: item.UID})
		}
	}

	remotes := &v1alpha2.RemoteMCPServerList{}
	if err := c.List(ctx, remotes, opts...); err != nil {
		return nil, 0, fmt.Errorf("list remote mcp servers by deployment id %s: %w", deploymentID, err)
	}

	sort.Slice(owners, func(i, j int) bool {
		if owners[i].kind != owners[j].kind {
			return owners[i].kind < owners[j].kind
		}
		return owners[i].name < owners[j].name
	})
	return owners, len(remotes.Items), nil
}

// indexWorkloadsByOwner maps owner UID -> Deployment. kagent sets
// controller=true on the ownerReference of Agent workloads, but kmcp writes a
// plain ownerReference (controller unset) on MCPServer workloads, so every
// ownerReference is indexed. A controller reference wins over a non-controller
// one when two Deployments name the same owner.
func indexWorkloadsByOwner(items []appsv1.Deployment) map[k8stypes.UID]*appsv1.Deployment {
	byOwner := make(map[k8stypes.UID]*appsv1.Deployment, len(items))
	isController := make(map[k8stypes.UID]bool, len(items))
	for i := range items {
		for _, ref := range items[i].OwnerReferences {
			ctrl := ref.Controller != nil && *ref.Controller
			if _, seen := byOwner[ref.UID]; seen && (isController[ref.UID] || !ctrl) {
				continue
			}
			byOwner[ref.UID] = &items[i]
			isController[ref.UID] = ctrl
		}
	}
	return byOwner
}

// kubernetesWorkloadAvailable mirrors `kubectl rollout status`: the current
// generation has been observed, every desired replica is updated and
// available, no old replicas remain, and the Available condition is True.
func kubernetesWorkloadAvailable(d *appsv1.Deployment) (available, stalled bool, detail string) {
	if d.Generation > d.Status.ObservedGeneration {
		return false, false, "waiting for rollout to be observed"
	}
	for _, cond := range d.Status.Conditions {
		if cond.Type == appsv1.DeploymentProgressing &&
			cond.Status == corev1.ConditionFalse &&
			cond.Reason == "ProgressDeadlineExceeded" {
			return false, true, fmt.Sprintf("rollout exceeded its progress deadline: %s", cond.Message)
		}
	}
	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	if d.Status.UpdatedReplicas < desired {
		return false, false, fmt.Sprintf("%d of %d replicas updated", d.Status.UpdatedReplicas, desired)
	}
	if d.Status.Replicas > d.Status.UpdatedReplicas {
		return false, false, fmt.Sprintf("%d old replicas pending termination", d.Status.Replicas-d.Status.UpdatedReplicas)
	}
	if d.Status.AvailableReplicas < d.Status.UpdatedReplicas {
		return false, false, fmt.Sprintf("%d of %d updated replicas available", d.Status.AvailableReplicas, d.Status.UpdatedReplicas)
	}
	for _, cond := range d.Status.Conditions {
		if cond.Type == appsv1.DeploymentAvailable {
			if cond.Status == corev1.ConditionTrue {
				return true, false, ""
			}
			return false, false, fmt.Sprintf("not available: %s", cond.Message)
		}
	}
	return false, false, "Available condition not reported yet"
}

// readyResult builds the Ready/Progressing pair for one probe outcome:
//   - True:    workloads available; Progressing=False/RolloutComplete.
//   - Unknown: rollout still converging; Progressing=True. Unknown (not
//     False) so clients render "deploying" rather than "failed".
//   - False:   rollout failed (e.g. progress deadline exceeded);
//     Progressing=False with the same reason.
func readyResult(readyStatus v1alpha1.ConditionStatus, reason, message string, gen int64, now time.Time) *types.ReadinessResult {
	progressingStatus := v1alpha1.ConditionTrue
	progressingReason := "Applied"
	progressingMessage := "kagent resources reconciled; waiting for rollout"
	switch readyStatus {
	case v1alpha1.ConditionTrue:
		progressingStatus = v1alpha1.ConditionFalse
		progressingReason = "RolloutComplete"
		progressingMessage = message
	case v1alpha1.ConditionFalse:
		progressingStatus = v1alpha1.ConditionFalse
		progressingReason = reason
		progressingMessage = message
	}
	return &types.ReadinessResult{
		Ready: readyStatus == v1alpha1.ConditionTrue,
		Conditions: []v1alpha1.Condition{{
			Type:               "Ready",
			Status:             readyStatus,
			Reason:             reason,
			Message:            message,
			LastTransitionTime: now,
			ObservedGeneration: gen,
		}, {
			Type:               "Progressing",
			Status:             progressingStatus,
			Reason:             progressingReason,
			Message:            progressingMessage,
			LastTransitionTime: now,
			ObservedGeneration: gen,
		}},
	}
}

var _ types.DeploymentReadinessProber = (*kubernetesDeploymentAdapter)(nil)
