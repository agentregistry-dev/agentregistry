package declarative

import (
	"context"
	"errors"
	"fmt"
	"time"

	cliCommon "github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// deploymentStatus is the shape emitted under .status when a deployment is
// rendered as YAML/JSON. Intentionally a CLI projection rather than the raw
// v1alpha1.Status conditions block so imperative users keep the compact fields
// they already consume while apply decode still ignores incoming status.
type deploymentStatus struct {
	ID               string         `json:"id,omitempty" yaml:"id,omitempty"`
	Phase            string         `json:"phase,omitempty" yaml:"phase,omitempty"`
	Origin           string         `json:"origin,omitempty" yaml:"origin,omitempty"`
	Error            string         `json:"error,omitempty" yaml:"error,omitempty"`
	ProviderMetadata map[string]any `json:"providerMetadata,omitempty" yaml:"providerMetadata,omitempty"`
	DeployedAt       time.Time      `json:"deployedAt,omitempty" yaml:"deployedAt,omitempty"`
	UpdatedAt        time.Time      `json:"updatedAt,omitempty" yaml:"updatedAt,omitempty"`
}

func listLatestAny[T v1alpha1.Object](ctx context.Context, kind string, newObj func() T) ([]any, error) {
	items, err := client.ListAllTyped(
		ctx,
		apiClient,
		kind,
		client.ListOpts{
			Namespace:  v1alpha1.DefaultNamespace,
			LatestOnly: true,
			Limit:      200,
		},
		newObj,
	)
	if err != nil {
		return nil, err
	}

	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return out, nil
}

func deleteAny[T v1alpha1.Object](ctx context.Context, kind, name, version string, force bool, newObj func() T) error {
	targetVersion := version
	if targetVersion == "" {
		obj, err := client.GetTyped(ctx, apiClient, kind, v1alpha1.DefaultNamespace, name, "", newObj)
		if err != nil {
			return err
		}
		targetVersion = obj.GetMetadata().Version
	}
	return apiClient.Delete(ctx, kind, v1alpha1.DefaultNamespace, name, targetVersion, client.DeleteOpts{Force: force})
}

func listDeploymentAny(ctx context.Context) ([]any, error) {
	deployments, err := cliCommon.ListDeployments(ctx, apiClient)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(deployments))
	for _, dep := range deployments {
		out = append(out, dep)
	}
	return out, nil
}

func getDeploymentByTarget(ctx context.Context, name string) (any, error) {
	deployments, err := cliCommon.ListDeployments(ctx, apiClient)
	if err != nil {
		return nil, err
	}
	for _, dep := range deployments {
		if dep != nil && dep.TargetName == name {
			return dep, nil
		}
	}
	return nil, database.ErrNotFound
}

func deleteDeploymentByTarget(ctx context.Context, name, version string, force bool) error {
	if version == "" {
		return fmt.Errorf("%w: --version is required when deleting deployments", database.ErrInvalidInput)
	}

	deployments, err := cliCommon.ListDeployments(ctx, apiClient)
	if err != nil {
		return fmt.Errorf("listing deployments: %w", err)
	}

	var matches []*cliCommon.DeploymentRecord
	for _, dep := range deployments {
		if dep == nil {
			continue
		}
		if dep.TargetName == name && dep.TargetVersion == version {
			matches = append(matches, dep)
		}
	}
	if len(matches) == 0 {
		return database.ErrNotFound
	}

	var errs []error
	for _, dep := range matches {
		if err := apiClient.Delete(ctx, v1alpha1.KindDeployment, dep.Namespace, dep.Name, dep.Version, client.DeleteOpts{Force: force}); err != nil {
			errs = append(errs, fmt.Errorf("deleting %s (provider %s): %w", dep.ID, dep.ProviderID, err))
		}
	}
	return errorsJoin(errs)
}

func deploymentToDocument(dep *cliCommon.DeploymentRecord) any {
	if dep == nil {
		return nil
	}

	targetKind := v1alpha1.KindAgent
	if dep.ResourceType == "mcp" {
		targetKind = v1alpha1.KindMCPServer
	}

	// metadata is the Deployment row's identity, NOT the target's. Two
	// deployments of the same target/version against different providers
	// are distinct rows; collapsing them onto target identity here
	// (previous behavior) made get-then-apply round-trips clobber the
	// wrong row and made delete by metadata identity impossible.
	return struct {
		APIVersion string                  `json:"apiVersion" yaml:"apiVersion"`
		Kind       string                  `json:"kind" yaml:"kind"`
		Metadata   v1alpha1.ObjectMeta     `json:"metadata" yaml:"metadata"`
		Spec       v1alpha1.DeploymentSpec `json:"spec" yaml:"spec"`
		Status     deploymentStatus        `json:"status,omitempty" yaml:"status,omitempty"`
	}{
		APIVersion: v1alpha1.GroupVersion,
		Kind:       v1alpha1.KindDeployment,
		Metadata: v1alpha1.ObjectMeta{
			Namespace: dep.Namespace,
			Name:      dep.Name,
			Version:   dep.Version,
		},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef: v1alpha1.ResourceRef{
				Kind:    targetKind,
				Name:    dep.TargetName,
				Version: dep.TargetVersion,
			},
			ProviderRef: v1alpha1.ResourceRef{
				Kind: v1alpha1.KindProvider,
				Name: dep.ProviderID,
			},
			Env:            dep.Env,
			ProviderConfig: dep.ProviderConfig,
		},
		Status: deploymentStatus{
			ID:               dep.ID,
			Phase:            dep.Status,
			Origin:           dep.Origin,
			Error:            dep.Error,
			ProviderMetadata: dep.ProviderMetadata,
			DeployedAt:       dep.CreatedAt,
			UpdatedAt:        dep.UpdatedAt,
		},
	}
}

func agentRow(agent *v1alpha1.Agent) []string {
	if agent == nil {
		return []string{"<invalid>"}
	}
	return []string{
		printer.TruncateString(agent.Metadata.Name, 40),
		agent.Metadata.Version,
		printer.EmptyValueOrDefault(agent.Spec.Framework, "<none>"),
		printer.EmptyValueOrDefault(agent.Spec.Language, "<none>"),
		printer.EmptyValueOrDefault(agent.Spec.ModelProvider, "<none>"),
		printer.TruncateString(printer.EmptyValueOrDefault(agent.Spec.ModelName, "<none>"), 30),
	}
}

func mcpRow(server *v1alpha1.MCPServer) []string {
	if server == nil {
		return []string{"<invalid>"}
	}
	return []string{
		printer.TruncateString(server.Metadata.Name, 40),
		server.Metadata.Version,
		printer.TruncateString(printer.EmptyValueOrDefault(server.Spec.Description, "<none>"), 60),
	}
}

func skillRow(skill *v1alpha1.Skill) []string {
	if skill == nil {
		return []string{"<invalid>"}
	}
	return []string{
		printer.TruncateString(skill.Metadata.Name, 40),
		skill.Metadata.Version,
		printer.TruncateString(printer.EmptyValueOrDefault(skill.Spec.Description, "<none>"), 60),
	}
}

func promptRow(prompt *v1alpha1.Prompt) []string {
	if prompt == nil {
		return []string{"<invalid>"}
	}
	return []string{
		printer.TruncateString(prompt.Metadata.Name, 40),
		prompt.Metadata.Version,
		printer.TruncateString(printer.EmptyValueOrDefault(prompt.Spec.Description, "<none>"), 60),
	}
}

func providerRow(provider *v1alpha1.Provider) []string {
	if provider == nil {
		return []string{"<invalid>"}
	}
	return []string{provider.Metadata.Name, provider.Spec.Platform}
}

func remoteMCPServerRow(r *v1alpha1.RemoteMCPServer) []string {
	if r == nil {
		return []string{"<invalid>"}
	}
	return []string{
		printer.TruncateString(r.Metadata.Name, 40),
		r.Metadata.Version,
		printer.EmptyValueOrDefault(r.Spec.Remote.Type, "<none>"),
		printer.TruncateString(printer.EmptyValueOrDefault(r.Spec.Remote.URL, "<none>"), 60),
	}
}

func deploymentRow(dep *cliCommon.DeploymentRecord) []string {
	if dep == nil {
		return []string{"<invalid>"}
	}
	return []string{
		dep.ID,
		dep.TargetName,
		dep.TargetVersion,
		dep.ResourceType,
		dep.ProviderID,
		dep.Status,
	}
}

func errorsJoin(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}
