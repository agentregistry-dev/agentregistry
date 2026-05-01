package common

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

const platformMetadataPrefix = "platforms.agentregistry.solo.io/"

// DeploymentRecord is the CLI-friendly projection of a v1alpha1 Deployment.
type DeploymentRecord struct {
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	ID        string `json:"id"`

	TargetName        string            `json:"serverName"`
	TargetVersion     string            `json:"targetVersion,omitempty"`
	ResourceType      string            `json:"resourceType"`
	ProviderID        string            `json:"providerId,omitempty"`
	Status            string            `json:"status"`
	Origin            string            `json:"origin"`
	Env               map[string]string `json:"env,omitempty"`
	ProviderConfig    map[string]any    `json:"providerConfig,omitempty"`
	ProviderMetadata  map[string]any    `json:"providerMetadata,omitempty"`
	Error             string            `json:"error,omitempty"`
	CreatedAt         time.Time         `json:"deployedAt,omitempty"`
	UpdatedAt         time.Time         `json:"updatedAt,omitempty"`
	DeletionTimestamp *time.Time        `json:"deletionTimestamp,omitempty"`
}

// ListDeployments returns every Deployment row visible from the default namespace.
func ListDeployments(ctx context.Context, c *client.Client) ([]*DeploymentRecord, error) {
	deployments, err := client.ListAllTyped(
		ctx,
		c,
		v1alpha1.KindDeployment,
		client.ListOpts{
			Namespace:          v1alpha1.DefaultNamespace,
			IncludeTerminating: true,
			Limit:              200,
		},
		func() *v1alpha1.Deployment { return &v1alpha1.Deployment{} },
	)
	if err != nil {
		return nil, err
	}

	out := make([]*DeploymentRecord, 0, len(deployments))
	for _, dep := range deployments {
		out = append(out, DeploymentRecordFromObject(dep))
	}
	return out, nil
}

// FindDeploymentByIDPrefix resolves a deployment identity by exact or prefix match.
func FindDeploymentByIDPrefix(ctx context.Context, c *client.Client, prefix string) (*DeploymentRecord, error) {
	deployments, err := ListDeployments(ctx, c)
	if err != nil {
		return nil, err
	}

	var matches []*DeploymentRecord
	for _, dep := range deployments {
		if dep != nil && strings.HasPrefix(dep.ID, prefix) {
			matches = append(matches, dep)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("deployment not found: %s", prefix)
	case 1:
		return matches[0], nil
	default:
		return nil, fmt.Errorf("ambiguous deployment ID prefix %q matches %d deployments", prefix, len(matches))
	}
}

// DeploymentRecordFromObject projects a v1alpha1 Deployment into the CLI view.
func DeploymentRecordFromObject(dep *v1alpha1.Deployment) *DeploymentRecord {
	if dep == nil {
		return nil
	}
	return &DeploymentRecord{
		Namespace:         dep.Metadata.NamespaceOrDefault(),
		Name:              dep.Metadata.Name,
		Version:           dep.Metadata.Version,
		ID:                DeploymentID(dep.Metadata.NamespaceOrDefault(), dep.Metadata.Name, dep.Metadata.Version),
		TargetName:        dep.Spec.TargetRef.Name,
		TargetVersion:     dep.Spec.TargetRef.Version,
		ResourceType:      deploymentResourceType(dep.Spec.TargetRef.Kind),
		ProviderID:        dep.Spec.ProviderRef.Name,
		Status:            DeploymentStatus(dep),
		Origin:            "managed",
		Env:               cloneStringMap(dep.Spec.Env),
		ProviderConfig:    cloneAnyMap(dep.Spec.ProviderConfig),
		ProviderMetadata:  deploymentProviderMetadata(dep.Metadata.Annotations),
		Error:             deploymentError(dep.Status),
		CreatedAt:         dep.Metadata.CreatedAt,
		UpdatedAt:         dep.Metadata.UpdatedAt,
		DeletionTimestamp: dep.Metadata.DeletionTimestamp,
	}
}

// DeploymentID is the display identity used by imperative deployment commands.
func DeploymentID(namespace, name, version string) string {
	return fmt.Sprintf("%s/%s/%s", namespace, name, version)
}

// DeploymentResourceName returns the generated metadata.name used by imperative
// deployment create flows for a (target, provider) pair.
func DeploymentResourceName(targetName, providerID string) string {
	name := strings.ReplaceAll(targetName, "/", "-")
	if providerID == "" {
		return name
	}
	return fmt.Sprintf("%s-%s", name, providerID)
}

// DeploymentStatus derives the old CLI phase strings from v1alpha1 conditions.
func DeploymentStatus(dep *v1alpha1.Deployment) string {
	if dep == nil {
		return "unknown"
	}
	if dep.Metadata.DeletionTimestamp != nil {
		return "terminating"
	}
	if dep.Status.IsConditionTrue("Ready") {
		return "deployed"
	}
	if c := dep.Status.GetCondition("Degraded"); c != nil && c.Status == v1alpha1.ConditionTrue {
		return "failed"
	}
	if dep.Spec.DesiredState == v1alpha1.DesiredStateUndeployed {
		return "undeployed"
	}
	if c := dep.Status.GetCondition("Progressing"); c != nil && c.Status != v1alpha1.ConditionFalse {
		return "deploying"
	}
	if c := dep.Status.GetCondition("ProviderConfigured"); c != nil && c.Status == v1alpha1.ConditionTrue {
		return "deploying"
	}
	return "pending"
}

func deploymentError(status v1alpha1.Status) string {
	for _, conditionType := range []string{"Degraded", "Ready", "Progressing"} {
		if c := status.GetCondition(conditionType); c != nil && c.Message != "" && c.Status != v1alpha1.ConditionTrue {
			return c.Message
		}
	}
	return ""
}

func deploymentProviderMetadata(annotations map[string]string) map[string]any {
	if len(annotations) == 0 {
		return nil
	}

	out := map[string]any{}
	shortKeys := map[string]bool{}
	for key, value := range annotations {
		if !strings.HasPrefix(key, platformMetadataPrefix) {
			continue
		}
		shortKey := key[strings.LastIndex(key, "/")+1:]
		if shortKey == "" || shortKeys[shortKey] {
			out[key] = value
			continue
		}
		shortKeys[shortKey] = true
		out[shortKey] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func deploymentResourceType(kind string) string {
	switch kind {
	case v1alpha1.KindMCPServer:
		return "mcp"
	default:
		return strings.ToLower(kind)
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	maps.Copy(out, in)
	return out
}
