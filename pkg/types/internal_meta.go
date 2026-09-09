package types

import (
	"encoding/json"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// DeploymentInternalMeta is durable Deployment state excluded from public APIs.
type DeploymentInternalMeta struct {
	// RuntimeID is the provider-assigned runtime identifier.
	RuntimeID string `json:"runtimeId,omitempty"`
	// RuntimeName is the provider-facing runtime name.
	RuntimeName string `json:"runtimeName,omitempty"`
	// RuntimeResourceID is the provider's globally qualified resource identifier.
	RuntimeResourceID string `json:"runtimeResourceId,omitempty"`
	// RuntimeNamespace is the provider-facing namespace containing the workload.
	RuntimeNamespace string `json:"runtimeNamespace,omitempty"`
	// ActorSubject is the identity presented by the deployed workload.
	ActorSubject string `json:"actorSubject,omitempty"`
	// AppProtocol is the protocol used to invoke the deployed workload.
	AppProtocol v1alpha1.AgentProtocol `json:"appProtocol,omitempty"`
	// LastAppliedFingerprint identifies the desired input accepted by the adapter.
	LastAppliedFingerprint string `json:"lastAppliedFingerprint,omitempty"`
	// LastForceToken records the force-reconcile request already processed.
	LastForceToken string `json:"lastForceToken,omitempty"`
	// DiscoveryMisses counts successful polls that consecutively omitted the workload.
	DiscoveryMisses int `json:"discoveryMisses,omitempty"`
}

// DeploymentRecord combines a Deployment with its internal state.
type DeploymentRecord struct {
	*v1alpha1.Deployment
	InternalMeta DeploymentInternalMeta
}

// DeploymentRecordFromRaw decodes a stored Deployment and its internal metadata.
func DeploymentRecordFromRaw(raw *v1alpha1.RawObject) (*DeploymentRecord, error) {
	deployment, err := v1alpha1.EnvelopeFromRaw(
		func() *v1alpha1.Deployment { return &v1alpha1.Deployment{} },
		raw,
		v1alpha1.KindDeployment,
	)
	if err != nil {
		return nil, err
	}
	record := &DeploymentRecord{Deployment: deployment}
	if len(raw.InternalMeta) > 0 {
		if err := json.Unmarshal(raw.InternalMeta, &record.InternalMeta); err != nil {
			return nil, fmt.Errorf("decode Deployment internal meta: %w", err)
		}
	}
	return record, nil
}
