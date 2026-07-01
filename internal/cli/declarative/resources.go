package declarative

import (
	"context"
	"errors"
	"fmt"

	cliCommon "github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
)

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
	ref, err := parseResourceLookupRef(name)
	if err != nil {
		return nil, err
	}
	items, err := client.ListTagsOfName(ctx, c, kind, ref.Namespace, ref.Name, newObj)
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
	ref, err := parseResourceLookupRef(name)
	if err != nil {
		return err
	}
	items, err := client.ListTagsOfName(ctx, c, kind, ref.Namespace, ref.Name, newObj)
	if err != nil {
		return err
	}
	var errs []error
	for _, item := range items {
		tag := item.GetMetadata().Tag
		if tag == "" {
			errs = append(errs, fmt.Errorf("%s/%s: listed tag row has empty metadata.tag", kind, name))
			continue
		}
		if err := c.Delete(ctx, kind, ref.Namespace, ref.Name, tag); err != nil {
			errs = append(errs, fmt.Errorf("%s/%s@%s: %w", kind, name, tag, err))
		}
	}
	return errorsJoin(errs)
}

func deleteAny[T v1alpha1.Object](ctx context.Context, c *client.Client, kind, name, tag string, newObj func() T) error {
	ref, err := parseResourceLookupRef(name)
	if err != nil {
		return err
	}
	targetTag := tag
	if targetTag == "" {
		obj, err := client.GetTyped(ctx, c, kind, ref.Namespace, ref.Name, "", newObj)
		if err != nil {
			return err
		}
		targetTag = obj.GetMetadata().Tag
	}
	return c.Delete(ctx, kind, ref.Namespace, ref.Name, targetTag)
}

func listDeploymentResources(ctx context.Context, c *client.Client, opts scheme.ListOpts) ([]any, error) {
	// Translate the CLI-facing origin into the server filter. Unset defaults
	// to managed to preserve historical `arctl get deployments` behavior
	// (and keep `get all` scoped to managed rows); "all" clears the filter
	// so the server returns both provenances.
	origin := opts.Origin
	switch origin {
	case "":
		origin = v1alpha1.DeploymentOriginManaged
	case "all":
		origin = ""
	}
	items, err := client.ListAllTyped(
		ctx,
		c,
		v1alpha1.KindDeployment,
		client.ListOpts{
			Namespace:          v1alpha1.DefaultNamespace,
			Limit:              200,
			Origin:             origin,
			IncludeTerminating: true,
		},
		func() *v1alpha1.Deployment { return &v1alpha1.Deployment{} },
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
