package kinds

import (
	"context"
	"fmt"
	"io"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"gopkg.in/yaml.v3"
)

// MakeApplyFunc creates a standard Apply function from a typed conversion + service call.
// Eliminates the repeated type-assert → dryRun → apply → result pattern used by
// agent, skill, prompt, and mcp kinds.
//
// Type parameters are inferred from the function arguments — callers pass service
// methods directly (e.g. agentService.ApplyAgent, agentService.GetAgentVersion)
// without wrapper closures.
//
// svcGet is called only during dry-run to check whether the resource already exists.
// Pass nil when existence checking is not supported; in that case dry-run always reports
// StatusCreated (safe default that matches kubectl apply --dry-run=server behaviour).
func MakeApplyFunc[Spec any, Req any, ApplyResp any, GetResp any](
	kind string,
	toReq func(Metadata, *Spec) *Req,
	svcApply func(context.Context, *Req) (*ApplyResp, error),
	svcGet func(context.Context, string, string) (*GetResp, error),
) ApplyFunc {
	return func(ctx context.Context, doc *Document, opts ApplyOpts) (*Result, error) {
		spec, ok := doc.Spec.(*Spec)
		if !ok {
			return nil, fmt.Errorf("%s: unexpected spec type %T", kind, doc.Spec)
		}
		result := &Result{Kind: kind, Name: doc.Metadata.Name, Version: doc.Metadata.Version}
		if opts.DryRun {
			if svcGet == nil {
				result.Status = StatusCreated
				return result, nil
			}
			_, err := svcGet(ctx, doc.Metadata.Name, doc.Metadata.Version)
			if err != nil {
				result.Status = StatusCreated // not found → would create
			} else {
				result.Status = StatusConfigured // found → would update (kubectl-style)
			}
			return result, nil
		}
		if _, err := svcApply(ctx, toReq(doc.Metadata, spec)); err != nil {
			return nil, err
		}
		result.Status = StatusApplied
		return result, nil
	}
}

// MakeGetFunc creates a standard Get function that dispatches by version presence.
func MakeGetFunc[Resp any](
	getLatest func(context.Context, string) (*Resp, error),
	getVersion func(context.Context, string, string) (*Resp, error),
) GetFunc {
	return func(ctx context.Context, name, version string) (any, error) {
		if version == "" {
			return getLatest(ctx, name)
		}
		return getVersion(ctx, name, version)
	}
}

// MakeDeleteFunc wraps a typed delete into the generic DeleteFunc signature.
func MakeDeleteFunc(del func(context.Context, string, string) error) DeleteFunc {
	return del
}

// MakeInitTemplate creates a standard init template writer from a default spec value.
func MakeInitTemplate[Spec any](kind string, defaultSpec Spec) InitTemplateFunc {
	return func(_ context.Context, out io.Writer, params InitParams) error {
		doc := struct {
			APIVersion string   `yaml:"apiVersion"`
			Kind       string   `yaml:"kind"`
			Metadata   Metadata `yaml:"metadata"`
			Spec       Spec     `yaml:"spec"`
		}{
			APIVersion: models.APIVersion,
			Kind:       kind,
			Metadata:   Metadata{Name: params.Name, Version: params.Version},
			Spec:       defaultSpec,
		}
		enc := yaml.NewEncoder(out)
		defer enc.Close()
		return enc.Encode(doc)
	}
}

// AppliedResult returns an applied Result. Used by kinds whose Apply functions
// have custom service-call logic (e.g. provider, deployment).
func AppliedResult(kind string, doc *Document) *Result {
	return &Result{
		Kind:    kind,
		Name:    doc.Metadata.Name,
		Version: doc.Metadata.Version,
		Status:  StatusApplied,
	}
}

// MakeListFunc creates a ListFunc that calls a typed (context-free) list function and
// converts the result to []any. Eliminates the repeated slice-conversion boilerplate
// used by agent, skill, prompt, and mcp kinds whose client methods take no extra args.
func MakeListFunc[T any](listFn func() ([]*T, error)) func(context.Context) ([]any, error) {
	return func(_ context.Context) ([]any, error) {
		items, err := listFn()
		if err != nil {
			return nil, err
		}
		out := make([]any, len(items))
		for i, x := range items {
			out[i] = x
		}
		return out, nil
	}
}

// AssertSpec type-asserts doc.Spec to *Spec and returns the typed pointer or an error.
// Used by kinds with custom Apply logic that still need the common type-assert pattern.
func AssertSpec[Spec any](kind string, doc *Document) (*Spec, error) {
	spec, ok := doc.Spec.(*Spec)
	if !ok {
		return nil, fmt.Errorf("%s: unexpected spec type %T", kind, doc.Spec)
	}
	return spec, nil
}
