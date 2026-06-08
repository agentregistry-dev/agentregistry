package declarative

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	cliCommon "github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
)

// deploymentStatus is the shape emitted under .status when a deployment is
// rendered as YAML/JSON. Intentionally a CLI projection rather than the raw
// v1alpha1.Status conditions block so imperative users keep the compact fields
// they already consume while apply decode still ignores incoming status.
type deploymentStatus struct {
	ID              string               `json:"id,omitempty" yaml:"id,omitempty"`
	Phase           string               `json:"phase,omitempty" yaml:"phase,omitempty"`
	Origin          string               `json:"origin,omitempty" yaml:"origin,omitempty"`
	Error           string               `json:"error,omitempty" yaml:"error,omitempty"`
	RuntimeMetadata map[string]any       `json:"runtimeMetadata,omitempty" yaml:"runtimeMetadata,omitempty"`
	DeployedAt      time.Time            `json:"deployedAt,omitempty" yaml:"deployedAt,omitempty"`
	UpdatedAt       time.Time            `json:"updatedAt,omitempty" yaml:"updatedAt,omitempty"`
	Conditions      []v1alpha1.Condition `json:"conditions,omitempty" yaml:"conditions,omitempty"`
	Details         json.RawMessage      `json:"details,omitempty" yaml:"details,omitempty"`
}

// listAny lists rows of the given kind. The zero scheme.ListOpts returns
// every (namespace, name, tag) row of the kind — same shape as a raw
// GET /v0/{plural}. Callers pass Tag or LatestOnly to filter; the CLI
// `get` command surfaces those as `--tag` / `--latest`.
//
// Earlier this helper hardcoded `LatestOnly: true`, which translated
// server-side to a literal `tag = "latest"` predicate. That returned
// nothing for resources published with explicit version tags, even
// though they existed in the registry. List now matches the natural
// "show me what's there" expectation.
func listAny[T v1alpha1.Object](ctx context.Context, c *client.Client, kind string, opts scheme.ListOpts, newObj func() T) ([]any, error) {
	items, err := client.ListAllTyped(
		ctx,
		c,
		kind,
		client.ListOpts{
			Namespace:  v1alpha1.DefaultNamespace,
			Tag:        opts.Tag,
			LatestOnly: opts.LatestOnly,
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

// listTagsAny lists artifact tags and erases the concrete envelope type so the
// table printer can format the rows.
func listTagsAny[T v1alpha1.Object](ctx context.Context, c *client.Client, kind, name string, newObj func() T) ([]any, error) {
	items, err := client.ListTagsOfName(ctx, c, kind, v1alpha1.DefaultNamespace, name, newObj)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return out, nil
}

// deleteAllTagsAny lists every live tag and deletes each exact tag so the
// imperative command can report tag-scoped failures while preserving the
// declarative DELETE /v0/apply contract for file input.
func deleteAllTagsAny[T v1alpha1.Object](ctx context.Context, c *client.Client, kind, name string, newObj func() T) error {
	items, err := listTagsAny(ctx, c, kind, name, newObj)
	if err != nil {
		return err
	}
	var errs []error
	for _, item := range items {
		obj, ok := item.(v1alpha1.Object)
		if !ok {
			errs = append(errs, fmt.Errorf("%s/%s: unexpected tag list item type %T", kind, name, item))
			continue
		}
		tag := obj.GetMetadata().Tag
		if tag == "" {
			errs = append(errs, fmt.Errorf("%s/%s: listed tag row has empty metadata.tag", kind, name))
			continue
		}
		if err := c.Delete(ctx, kind, v1alpha1.DefaultNamespace, name, tag); err != nil {
			errs = append(errs, fmt.Errorf("%s/%s@%s: %w", kind, name, tag, err))
		}
	}
	return errorsJoin(errs)
}

func deleteAny[T v1alpha1.Object](ctx context.Context, c *client.Client, kind, name, tag string, newObj func() T) error {
	targetTag := tag
	if targetTag == "" {
		obj, err := client.GetTyped(ctx, c, kind, v1alpha1.DefaultNamespace, name, "", newObj)
		if err != nil {
			return err
		}
		targetTag = obj.GetMetadata().Tag
	}
	return c.Delete(ctx, kind, v1alpha1.DefaultNamespace, name, targetTag)
}

func listDeploymentAny(ctx context.Context, c *client.Client) ([]any, error) {
	deployments, err := cliCommon.ListDeployments(ctx, c)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(deployments))
	for _, dep := range deployments {
		out = append(out, dep)
	}
	return out, nil
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
	// deployments of the same target/tag against different runtimes
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
		},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef: v1alpha1.ResourceRef{
				Kind: targetKind,
				Name: dep.TargetName,
				Tag:  dep.TargetTag,
			},
			RuntimeRef: v1alpha1.ResourceRef{
				Kind: v1alpha1.KindRuntime,
				Name: dep.RuntimeID,
			},
			Env:           dep.Env,
			RuntimeConfig: dep.RuntimeConfig,
		},
		Status: deploymentStatus{
			ID:              dep.ID,
			Phase:           dep.Status,
			Origin:          dep.Origin,
			Error:           dep.Error,
			RuntimeMetadata: dep.RuntimeMetadata,
			DeployedAt:      dep.CreatedAt,
			UpdatedAt:       dep.UpdatedAt,
			Conditions:      dep.Conditions,
			Details:         dep.Details,
		},
	}
}

func agentRow(agent *v1alpha1.Agent) []string {
	if agent == nil {
		return []string{"<invalid>"}
	}
	return []string{
		printer.TruncateString(agent.Metadata.Name, 40),
		agent.Metadata.Tag,
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
		server.Metadata.Tag,
		printer.TruncateString(printer.EmptyValueOrDefault(server.Spec.Description, "<none>"), 60),
	}
}

func skillRow(skill *v1alpha1.Skill) []string {
	if skill == nil {
		return []string{"<invalid>"}
	}
	return []string{
		printer.TruncateString(skill.Metadata.Name, 40),
		skill.Metadata.Tag,
		printer.TruncateString(printer.EmptyValueOrDefault(skill.Spec.Description, "<none>"), 60),
	}
}

func promptRow(prompt *v1alpha1.Prompt) []string {
	if prompt == nil {
		return []string{"<invalid>"}
	}
	return []string{
		printer.TruncateString(prompt.Metadata.Name, 40),
		prompt.Metadata.Tag,
		printer.TruncateString(printer.EmptyValueOrDefault(prompt.Spec.Description, "<none>"), 60),
	}
}

func runtimeRow(runtime *v1alpha1.Runtime) []string {
	if runtime == nil {
		return []string{"<invalid>"}
	}
	return []string{runtime.Metadata.Name, runtime.Spec.Type}
}

func deploymentRow(dep *cliCommon.DeploymentRecord) []string {
	if dep == nil {
		return []string{"<invalid>"}
	}
	return []string{
		dep.ID,
		dep.TargetName,
		dep.TargetTag,
		dep.ResourceType,
		dep.RuntimeID,
		dep.Status,
	}
}

func errorsJoin(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}
