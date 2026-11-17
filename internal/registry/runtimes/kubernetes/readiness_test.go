package kubernetes

import (
	"context"
	"strings"
	"testing"

	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
	kmcpv1alpha1 "github.com/kagent-dev/kmcp/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	adapterpkgtypes "github.com/agentregistry-dev/agentregistry/pkg/types"
)

const readinessTestNamespace = "kagent"

func readinessInput(deploymentID string) adapterpkgtypes.ApplyInput {
	return adapterpkgtypes.ApplyInput{
		Deployment: &v1alpha1.Deployment{
			Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: deploymentID, Generation: 3},
		},
		Runtime: &v1alpha1.Runtime{
			Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "kube"},
			Spec: v1alpha1.RuntimeSpec{
				Type:   v1alpha1.TypeKubernetes,
				Config: map[string]any{"namespace": readinessTestNamespace},
			},
		},
	}
}

func readinessAgent(name, deploymentID string, uid k8stypes.UID) *v1alpha2.Agent {
	return &v1alpha2.Agent{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: readinessTestNamespace, UID: uid,
		Labels: kubernetesDeploymentManagedLabels(deploymentID),
	}}
}

func readinessMCPServer(name, deploymentID string, uid k8stypes.UID) *kmcpv1alpha1.MCPServer {
	return &kmcpv1alpha1.MCPServer{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: readinessTestNamespace, UID: uid,
		Labels: kubernetesDeploymentManagedLabels(deploymentID),
	}}
}

type workloadState struct {
	replicas, updated, available, total int32
	availableCond                       corev1.ConditionStatus
	generation, observed                int64
	stalled                             bool
}

func healthy() workloadState {
	return workloadState{replicas: 1, updated: 1, available: 1, total: 1,
		availableCond: corev1.ConditionTrue, generation: 1, observed: 1}
}

func readinessWorkload(name, ownerKind string, ownerUID k8stypes.UID, s workloadState) *appsv1.Deployment {
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: readinessTestNamespace, Generation: s.generation,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "kagent.dev/v1alpha2", Kind: ownerKind, Name: name,
				UID: ownerUID, Controller: ptr.To(true),
			}},
		},
		Spec: appsv1.DeploymentSpec{Replicas: ptr.To(s.replicas)},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: s.observed,
			Replicas:           s.total,
			UpdatedReplicas:    s.updated,
			AvailableReplicas:  s.available,
		},
	}
	if s.availableCond != "" {
		d.Status.Conditions = append(d.Status.Conditions, appsv1.DeploymentCondition{
			Type: appsv1.DeploymentAvailable, Status: s.availableCond, Message: "availability",
		})
	}
	if s.stalled {
		d.Status.Conditions = append(d.Status.Conditions, appsv1.DeploymentCondition{
			Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse,
			Reason: "ProgressDeadlineExceeded", Message: "ReplicaSet has timed out progressing",
		})
	}
	return d
}

func requireReadyCondition(t *testing.T, result *adapterpkgtypes.ReadinessResult, wantStatus v1alpha1.ConditionStatus, wantReason string) {
	t.Helper()
	if result == nil {
		t.Fatal("ProbeReadiness returned nil result")
	}
	if wantReady := wantStatus == v1alpha1.ConditionTrue; result.Ready != wantReady {
		t.Fatalf("Ready = %v, want %v (conditions %+v)", result.Ready, wantReady, result.Conditions)
	}
	var ready, progressing *v1alpha1.Condition
	for i := range result.Conditions {
		switch result.Conditions[i].Type {
		case "Ready":
			ready = &result.Conditions[i]
		case "Progressing":
			progressing = &result.Conditions[i]
		}
	}
	if ready == nil || progressing == nil {
		t.Fatalf("expected Ready and Progressing conditions, got %+v", result.Conditions)
	}
	// Only a converging rollout (Ready=Unknown) is still Progressing.
	wantProgressing := v1alpha1.ConditionFalse
	if wantStatus == v1alpha1.ConditionUnknown {
		wantProgressing = v1alpha1.ConditionTrue
	}
	if ready.Status != wantStatus || ready.Reason != wantReason {
		t.Fatalf("Ready = %s/%s (%q), want %s/%s", ready.Status, ready.Reason, ready.Message, wantStatus, wantReason)
	}
	if ready.ObservedGeneration != 3 {
		t.Fatalf("Ready.ObservedGeneration = %d, want 3", ready.ObservedGeneration)
	}
	if progressing.Status != wantProgressing {
		t.Fatalf("Progressing = %s, want %s", progressing.Status, wantProgressing)
	}
}

func TestProbeReadiness_AgentWorkloadStates(t *testing.T) {
	const (
		isTrue    = v1alpha1.ConditionTrue
		isUnknown = v1alpha1.ConditionUnknown
		isFalse   = v1alpha1.ConditionFalse
	)
	tests := []struct {
		name       string
		workload   func() *workloadState
		wantStatus v1alpha1.ConditionStatus
		wantReason string
		wantInMsg  string
	}{
		{"available", func() *workloadState { s := healthy(); return &s }, isTrue, readinessReasonAvailable, "available"},
		{"workload not created", func() *workloadState { return nil }, isUnknown, readinessReasonWaiting, "workload not created yet"},
		{"replicas not available", func() *workloadState {
			s := healthy()
			s.available = 0
			s.availableCond = corev1.ConditionFalse
			return &s
		}, isUnknown, readinessReasonWaiting, "0 of 1 updated replicas available"},
		{"new generation not observed", func() *workloadState {
			s := healthy()
			s.generation = 2
			return &s
		}, isUnknown, readinessReasonWaiting, "waiting for rollout to be observed"},
		{"old replicas still running", func() *workloadState {
			s := healthy()
			s.total = 2
			return &s
		}, isUnknown, readinessReasonWaiting, "1 old replicas pending termination"},
		{"available condition missing", func() *workloadState {
			s := healthy()
			s.availableCond = ""
			return &s
		}, isUnknown, readinessReasonWaiting, "Available condition not reported yet"},
		{"rollout stalled", func() *workloadState {
			s := healthy()
			s.available = 0
			s.stalled = true
			return &s
		}, isFalse, readinessReasonRolloutStalled, "progress deadline"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objs := []client.Object{readinessAgent("myagent", "myagent", "agent-uid")}
			if s := tt.workload(); s != nil {
				objs = append(objs, readinessWorkload("myagent", "Agent", "agent-uid", *s))
			}
			withFakeKubeClient(t, objs...)

			result, err := NewKubernetesDeploymentAdapter().ProbeReadiness(context.Background(), readinessInput("myagent"))
			if err != nil {
				t.Fatalf("ProbeReadiness: %v", err)
			}
			requireReadyCondition(t, result, tt.wantStatus, tt.wantReason)
			if msg := result.Conditions[0].Message; !strings.Contains(msg, tt.wantInMsg) {
				t.Fatalf("Ready message %q does not contain %q", msg, tt.wantInMsg)
			}
		})
	}
}

func TestProbeReadiness_RequiresEveryOwnedWorkload(t *testing.T) {
	notAvailable := healthy()
	notAvailable.available = 0
	withFakeKubeClient(t,
		readinessAgent("myagent", "myagent", "agent-uid"),
		readinessWorkload("myagent", "Agent", "agent-uid", healthy()),
		readinessMCPServer("tools", "myagent", "mcp-uid"),
		readinessWorkload("tools", "MCPServer", "mcp-uid", notAvailable),
	)
	adapter := NewKubernetesDeploymentAdapter()
	result, err := adapter.ProbeReadiness(context.Background(), readinessInput("myagent"))
	if err != nil {
		t.Fatalf("ProbeReadiness: %v", err)
	}
	requireReadyCondition(t, result, v1alpha1.ConditionUnknown, readinessReasonWaiting)
	if msg := result.Conditions[0].Message; !strings.Contains(msg, "MCPServer tools") || strings.Contains(msg, "Agent myagent") {
		t.Fatalf("message should name only the pending MCP server, got %q", msg)
	}
}

// kmcp sets a plain ownerReference (controller unset) on MCPServer workloads.
func TestProbeReadiness_KmcpNonControllerOwnerReference(t *testing.T) {
	kmcpWorkload := func(name string, uid k8stypes.UID) *appsv1.Deployment {
		d := readinessWorkload(name, "MCPServer", uid, healthy())
		d.OwnerReferences[0].APIVersion = "kagent.dev/v1alpha1"
		d.OwnerReferences[0].Controller = nil
		return d
	}
	withFakeKubeClient(t,
		readinessAgent("incident-agent", "incident-agent", "agent-uid"),
		readinessWorkload("incident-agent", "Agent", "agent-uid", healthy()),
		readinessMCPServer("classifier-mcp-incident-agent", "incident-agent", "mcp-uid"),
		kmcpWorkload("classifier-mcp-incident-agent", "mcp-uid"),
	)
	result, err := NewKubernetesDeploymentAdapter().ProbeReadiness(context.Background(), readinessInput("incident-agent"))
	if err != nil {
		t.Fatalf("ProbeReadiness: %v", err)
	}
	requireReadyCondition(t, result, v1alpha1.ConditionTrue, readinessReasonAvailable)
}

func TestProbeReadiness_IgnoresOtherDeploymentsAndUnownedWorkloads(t *testing.T) {
	withFakeKubeClient(t,
		readinessAgent("myagent", "myagent", "agent-uid"),
		// A healthy workload with the same name but a different owner UID must not count.
		readinessWorkload("myagent", "Agent", "someone-else", healthy()),
		readinessAgent("other", "other-deployment", "other-uid"),
		readinessWorkload("other", "Agent", "other-uid", healthy()),
	)
	result, err := NewKubernetesDeploymentAdapter().ProbeReadiness(context.Background(), readinessInput("myagent"))
	if err != nil {
		t.Fatalf("ProbeReadiness: %v", err)
	}
	requireReadyCondition(t, result, v1alpha1.ConditionUnknown, readinessReasonWaiting)
}

func TestProbeReadiness_RemoteMCPServerOnly(t *testing.T) {
	withFakeKubeClient(t, &v1alpha2.RemoteMCPServer{ObjectMeta: metav1.ObjectMeta{
		Name: "weather", Namespace: readinessTestNamespace,
		Labels: kubernetesDeploymentManagedLabels("weather"),
	}})
	result, err := NewKubernetesDeploymentAdapter().ProbeReadiness(context.Background(), readinessInput("weather"))
	if err != nil {
		t.Fatalf("ProbeReadiness: %v", err)
	}
	requireReadyCondition(t, result, v1alpha1.ConditionTrue, readinessReasonRemoteMCPApplied)
}

func TestProbeReadiness_NoResources(t *testing.T) {
	withFakeKubeClient(t)
	result, err := NewKubernetesDeploymentAdapter().ProbeReadiness(context.Background(), readinessInput("ghost"))
	if err != nil {
		t.Fatalf("ProbeReadiness: %v", err)
	}
	requireReadyCondition(t, result, v1alpha1.ConditionUnknown, readinessReasonNoWorkload)
}
