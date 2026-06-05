package types

import (
	"context"
	"time"
)

const (
	// ConditionTypeSourceOutOfSync is set on Deployment.Status when the
	// source revision used by the last explicit deploy differs from the
	// latest observed source revision. It is status-only evidence; it does
	// not request or imply an automatic redeploy.
	ConditionTypeSourceOutOfSync = "SourceOutOfSync"

	ReasonSourceRevisionAligned     = "SourceRevisionAligned"
	ReasonSourceRevisionChanged     = "SourceRevisionChanged"
	ReasonSourceRevisionPending     = "SourceRevisionPending"
	ReasonSourceRevisionCheckFailed = "SourceRevisionCheckFailed"

	StatusDetailsKeySourceRevision = "sourceRevision"
)

// DeploymentSourceObserver lets a deployment adapter report source revision
// state without making that state part of desired apply fingerprinting.
type DeploymentSourceObserver interface {
	ObserveDeploymentSource(ctx context.Context, in ApplyInput) (*DeploymentSourceObservation, error)
}

// DeploymentSourceRef identifies the mutable source backing a Deployment.
// Git-backed adapters should set Type="git" and the repository fields.
type DeploymentSourceRef struct {
	Type          string `json:"type,omitempty"`
	RepositoryURL string `json:"repositoryUrl,omitempty"`
	Branch        string `json:"branch,omitempty"`
	Commit        string `json:"commit,omitempty"`
	Workdir       string `json:"workdir,omitempty"`
}

// DeploymentSourceObservation is the adapter-owned source evidence returned
// to the generic monitor.
type DeploymentSourceObservation struct {
	Platform        string              `json:"platform,omitempty"`
	SourceRef       DeploymentSourceRef `json:"sourceRef,omitempty"`
	AppliedRevision string              `json:"appliedRevision,omitempty"`
	LatestRevision  string              `json:"latestRevision,omitempty"`
}

// DeploymentSourceRevisionDetails is stored under
// status.details.sourceRevision.
type DeploymentSourceRevisionDetails struct {
	Platform        string              `json:"platform,omitempty"`
	SourceRef       DeploymentSourceRef `json:"sourceRef,omitempty"`
	AppliedRevision string              `json:"appliedRevision,omitempty"`
	LatestRevision  string              `json:"latestRevision,omitempty"`
	ObservedAt      time.Time           `json:"observedAt,omitempty"`
	Error           string              `json:"error,omitempty"`
}
