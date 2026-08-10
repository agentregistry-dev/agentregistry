package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/bundle"
	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/compose"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// Component-resolution sentinels. Missing/invalid are TERMINAL (the user must
// change the spec); pending is RETRYABLE (the referenced object's own
// controller will pin it, so rate-limited backoff converges).
var (
	errComponentMissing  = errors.New("plugin component missing")
	errComponentInvalid  = errors.New("plugin component invalid")
	errComponentsPending = errors.New("plugin components pending")
)

// componentGetter is the read-only, per-kind store access component
// resolution needs. *v1alpha1store.Store satisfies it.
type componentGetter interface {
	Get(ctx context.Context, namespace, name, tag string) (*v1alpha1.RawObject, error)
}

// treeFetcher fetches a git tree pinned at a commit (a referenced Skill's
// repo). source.FetchGitTree in production; a fake in tests.
type treeFetcher func(ctx context.Context, repo *v1alpha1.Repository, commit string) (*bundle.CanonicalBundle, error)

// resolveComponents resolves every composition ref on p to (a) the compose
// inputs carrying the actual content and (b) the pin set recorded in status.
// Pins are returned in spec order: skills, mcpServers, commands, instructions.
func (c *PluginController) resolveComponents(ctx context.Context, p *v1alpha1.Plugin) (compose.Inputs, []v1alpha1.PluginResolvedComponent, error) {
	in := compose.Inputs{
		PluginName:  p.Metadata.Name,
		Title:       p.Spec.Title,
		Description: p.Spec.Description,
	}
	var pins []v1alpha1.PluginResolvedComponent
	ns := p.Metadata.NamespaceOrDefault()

	for _, ref := range p.Spec.Skills {
		skill, pin, err := c.resolveSkill(ctx, ns, ref)
		if err != nil {
			return in, nil, err
		}
		in.Skills = append(in.Skills, skill)
		pins = append(pins, pin)
	}
	for _, ref := range p.Spec.MCPServers {
		server, pin, err := c.resolveMCPServer(ctx, ns, ref)
		if err != nil {
			return in, nil, err
		}
		in.MCPServers = append(in.MCPServers, server)
		pins = append(pins, pin)
	}
	for _, ref := range p.Spec.Commands {
		body, pin, err := c.resolvePrompt(ctx, ns, ref)
		if err != nil {
			return in, nil, err
		}
		in.Commands = append(in.Commands, compose.Command{Name: ref.Name, Body: body})
		pins = append(pins, pin)
	}
	if ref := p.Spec.Instructions; ref != nil {
		body, pin, err := c.resolvePrompt(ctx, ns, *ref)
		if err != nil {
			return in, nil, err
		}
		in.Instructions = &compose.Instructions{Name: ref.Name, Body: body}
		pins = append(pins, pin)
	}
	return in, pins, nil
}

// resolveSkill gates on the referenced Skill's own resolve-and-pin (its
// controller writes status.resolvedSource.commit) and fetches the pinned tree.
func (c *PluginController) resolveSkill(ctx context.Context, ns string, ref v1alpha1.ComponentRef) (compose.Skill, v1alpha1.PluginResolvedComponent, error) {
	raw, err := c.getComponent(ctx, v1alpha1.KindSkill, ns, ref)
	if err != nil {
		return compose.Skill{}, v1alpha1.PluginResolvedComponent{}, err
	}
	skill, err := v1alpha1.EnvelopeFromRaw(func() *v1alpha1.Skill { return &v1alpha1.Skill{} }, raw, v1alpha1.KindSkill)
	if err != nil {
		return compose.Skill{}, v1alpha1.PluginResolvedComponent{}, fmt.Errorf("%w: decode skill %s: %v", errComponentInvalid, componentID(v1alpha1.KindSkill, ns, ref), err)
	}
	if skill.Spec.Source == nil || skill.Spec.Source.Repository == nil {
		return compose.Skill{}, v1alpha1.PluginResolvedComponent{}, fmt.Errorf("%w: skill %s has no git source", errComponentInvalid, componentID(v1alpha1.KindSkill, ns, ref))
	}
	if skill.Status.ResolvedSource == nil || skill.Status.ResolvedSource.Commit == "" {
		return compose.Skill{}, v1alpha1.PluginResolvedComponent{}, fmt.Errorf("%w: skill %s not yet resolved", errComponentsPending, componentID(v1alpha1.KindSkill, ns, ref))
	}
	commit := skill.Status.ResolvedSource.Commit
	tree, err := c.fetchTree(ctx, skill.Spec.Source.Repository, commit)
	if err != nil {
		return compose.Skill{}, v1alpha1.PluginResolvedComponent{}, fmt.Errorf("fetch skill %s@%s: %w", componentID(v1alpha1.KindSkill, ns, ref), commit, err)
	}
	return compose.Skill{Name: ref.Name, Files: tree.Files},
		componentPin(v1alpha1.KindSkill, ns, ref, commit, ""), nil
}

// resolveMCPServer maps the referenced spec to its .mcp.json entry; shapes
// with no faithful desktop form are terminal.
func (c *PluginController) resolveMCPServer(ctx context.Context, ns string, ref v1alpha1.ComponentRef) (compose.MCPServer, v1alpha1.PluginResolvedComponent, error) {
	raw, err := c.getComponent(ctx, v1alpha1.KindMCPServer, ns, ref)
	if err != nil {
		return compose.MCPServer{}, v1alpha1.PluginResolvedComponent{}, err
	}
	server, err := v1alpha1.EnvelopeFromRaw(func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} }, raw, v1alpha1.KindMCPServer)
	if err != nil {
		return compose.MCPServer{}, v1alpha1.PluginResolvedComponent{}, fmt.Errorf("%w: decode mcp server %s: %v", errComponentInvalid, componentID(v1alpha1.KindMCPServer, ns, ref), err)
	}
	entry, err := compose.MCPEntryFromSpec(&server.Spec)
	if err != nil {
		return compose.MCPServer{}, v1alpha1.PluginResolvedComponent{}, fmt.Errorf("%w: mcp server %s: %v", errComponentInvalid, componentID(v1alpha1.KindMCPServer, ns, ref), err)
	}
	return compose.MCPServer{Name: ref.Name, Entry: entry},
		componentPin(v1alpha1.KindMCPServer, ns, ref, "", specHash(raw)), nil
}

// resolvePrompt loads an inline Prompt body (commands and instructions).
func (c *PluginController) resolvePrompt(ctx context.Context, ns string, ref v1alpha1.ComponentRef) (string, v1alpha1.PluginResolvedComponent, error) {
	raw, err := c.getComponent(ctx, v1alpha1.KindPrompt, ns, ref)
	if err != nil {
		return "", v1alpha1.PluginResolvedComponent{}, err
	}
	prompt, err := v1alpha1.EnvelopeFromRaw(func() *v1alpha1.Prompt { return &v1alpha1.Prompt{} }, raw, v1alpha1.KindPrompt)
	if err != nil {
		return "", v1alpha1.PluginResolvedComponent{}, fmt.Errorf("%w: decode prompt %s: %v", errComponentInvalid, componentID(v1alpha1.KindPrompt, ns, ref), err)
	}
	return prompt.Spec.Content, componentPin(v1alpha1.KindPrompt, ns, ref, "", specHash(raw)), nil
}

// getComponent reads one referenced object, defaulting namespace to the
// plugin's and a blank tag to the literal latest tag.
func (c *PluginController) getComponent(ctx context.Context, kind, pluginNS string, ref v1alpha1.ComponentRef) (*v1alpha1.RawObject, error) {
	getter := c.Components[kind]
	if getter == nil {
		return nil, fmt.Errorf("plugin controller: no store for component kind %s", kind)
	}
	ns, tag := componentNS(pluginNS, ref), componentTag(ref)
	raw, err := getter.Get(ctx, ns, ref.Name, tag)
	if errors.Is(err, pkgdb.ErrNotFound) {
		return nil, fmt.Errorf("%w: %s %s/%s:%s not found", errComponentMissing, kind, ns, ref.Name, tag)
	}
	if err != nil {
		return nil, fmt.Errorf("plugin controller: load %s %s/%s:%s: %w", kind, ns, ref.Name, tag, err) // retryable
	}
	return raw, nil
}

func componentNS(pluginNS string, ref v1alpha1.ComponentRef) string {
	if ref.Namespace != "" {
		return ref.Namespace
	}
	return pluginNS
}

func componentTag(ref v1alpha1.ComponentRef) string {
	if ref.Tag != "" {
		return ref.Tag
	}
	return v1alpha1store.DefaultTag()
}

func componentID(kind, pluginNS string, ref v1alpha1.ComponentRef) string {
	return fmt.Sprintf("%s %s/%s:%s", kind, componentNS(pluginNS, ref), ref.Name, componentTag(ref))
}

func componentPin(kind, pluginNS string, ref v1alpha1.ComponentRef, commit, contentHash string) v1alpha1.PluginResolvedComponent {
	return v1alpha1.PluginResolvedComponent{
		Kind:        kind,
		Namespace:   componentNS(pluginNS, ref),
		Name:        ref.Name,
		Tag:         componentTag(ref),
		Commit:      commit,
		ContentHash: contentHash,
	}
}

// specHash pins an inline content kind: sha256 of the stored spec bytes.
func specHash(raw *v1alpha1.RawObject) string {
	sum := sha256.Sum256(raw.Spec)
	return hex.EncodeToString(sum[:])
}

// classifyComponentErr maps a component-resolution error to a status reason
// and terminality; falls back to the source classifier for fetch errors.
func classifyComponentErr(err error) (reason string, terminal bool) {
	switch {
	case errors.Is(err, errComponentMissing):
		return "ComponentMissing", true
	case errors.Is(err, errComponentInvalid):
		return "ComponentInvalid", true
	case errors.Is(err, errComponentsPending):
		return "ComponentsPending", false
	default:
		return classifyResolveErr(err)
	}
}
