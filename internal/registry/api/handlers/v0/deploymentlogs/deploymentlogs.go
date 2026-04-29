// Package deploymentlogs owns the Deployment logs subresource:
// `/v0/deployments/{name}/{version}/logs`. Drains
// adapter.Logs through the Coordinator and returns the captured
// lines as JSON. The endpoint is bound to one specific kind
// (Deployment); the rest of the v1alpha1 CRUD surface lives in
// crud.
package deploymentlogs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/danielgtaylor/huma/v2"

	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// Config bundles the inputs for Register. The coordinator drives
// adapter.Logs; the store fetches the Deployment row so the endpoint
// can reject 404s early.
type Config struct {
	BasePrefix  string
	Store       *v1alpha1store.Store
	Coordinator *deploymentsvc.Coordinator
	// Authorize gates the request the same way the regular Deployment
	// GET handler does. nil means no gate. Logs leak runtime
	// stdout/stderr — frequently containing PII or secrets — so a
	// missing hook would let any authenticated caller read any
	// tenant's deployment logs regardless of role grants.
	//
	// Wire from PerKindHooks.Authorizers[KindDeployment] at router
	// boot. Verb is "get" so role mappings line up with the regular
	// Deployment GET handler.
	Authorize func(ctx context.Context, in resource.AuthorizeInput) error
}

// deploymentLogsInput is the request body — query flags for the stream +
// path segments for the deployment identity. Namespace rides on the
// ?namespace= query to match the main resource handler shape.
type deploymentLogsInput struct {
	Namespace string `query:"namespace" doc:"Namespace (internal; defaults to 'default')."`
	Name      string `path:"name"`
	Version   string `path:"version"`
	Follow    bool   `query:"follow" doc:"Stream indefinitely until client disconnects."`
	TailLines int    `query:"tailLines" doc:"Max backlog lines before live tail; 0 = unbounded."`
}

type deploymentLogLine struct {
	Timestamp string `json:"timestamp,omitempty" doc:"RFC3339 timestamp."`
	Stream    string `json:"stream,omitempty"     doc:"stdout | stderr | platform-specific."`
	Line      string `json:"line"                 doc:"Single log record."`
}

// maxLogLines caps the response payload in non-follow mode so a chatty
// adapter can't OOM the server. Picked to keep payloads under ~10 MB
// at typical log line sizes; bump in lockstep with the SSE port if
// callers need denser drains.
const maxLogLines = 10000

type deploymentLogsOutput struct {
	Body struct {
		Lines []deploymentLogLine `json:"lines"`
	}
}

// Register wires GET
// {basePrefix}/deployments/{name}/{version}/logs?namespace=default. The
// response is a JSON payload of log records drained from
// coordinator.Logs; follow=true keeps the channel open until the client
// disconnects (or until the adapter's context is cancelled).
//
// Non-streaming for now — huma lacks first-class SSE output and the
// kubernetes/local adapters still return closed channels. When real log
// streaming lands upstream, swap this for an SSE/chunked handler at the
// same path without touching the coordinator surface.
func Register(api huma.API, cfg Config) {
	path := cfg.BasePrefix + "/deployments/{name}/{version}/logs"

	huma.Register(api, huma.Operation{
		OperationID: "get-deployment-logs",
		Method:      http.MethodGet,
		Path:        path,
		Summary:     "Stream logs from a deployment's runtime workload",
	}, func(ctx context.Context, in *deploymentLogsInput) (*deploymentLogsOutput, error) {
		// follow=true is a streaming hint that this handler can't honor —
		// huma serializes the full body before responding, so a true
		// stream would buffer until the channel closes (or never, if the
		// adapter follows indefinitely) and OOM the process. Reject
		// upfront with a clear error; SSE/chunked support will land in
		// a separate endpoint.
		if in.Follow {
			return nil, huma.Error400BadRequest("follow=true is not supported on this endpoint; the streaming SSE variant is tracked as a follow-up")
		}
		ns := in.Namespace
		if ns == "" {
			ns = v1alpha1.DefaultNamespace
		}
		// Names allow `/` so callers must `%2F`-escape them on the wire;
		// Huma keeps the captures raw, so unescape before consulting
		// the Store.
		name, err := url.PathUnescape(in.Name)
		if err != nil {
			return nil, huma.Error400BadRequest(fmt.Sprintf("invalid name path segment: %v", err))
		}
		version, err := url.PathUnescape(in.Version)
		if err != nil {
			return nil, huma.Error400BadRequest(fmt.Sprintf("invalid version path segment: %v", err))
		}
		if cfg.Authorize != nil {
			if err := cfg.Authorize(ctx, resource.AuthorizeInput{
				Verb: "get", Kind: v1alpha1.KindDeployment,
				Namespace: ns, Name: name, Version: version,
			}); err != nil {
				return nil, err
			}
		}
		row, err := cfg.Store.Get(ctx, ns, name, version)
		if err != nil {
			if errors.Is(err, pkgdb.ErrNotFound) {
				return nil, huma.Error404NotFound(fmt.Sprintf("Deployment %q/%q@%q not found", ns, name, version))
			}
			return nil, huma.Error500InternalServerError("fetch Deployment", err)
		}
		deployment := &v1alpha1.Deployment{}
		deployment.SetTypeMeta(v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindDeployment})
		deployment.SetMetadata(row.Metadata)
		if len(row.Status) > 0 {
			if err := deployment.UnmarshalStatus(row.Status); err != nil {
				return nil, huma.Error500InternalServerError("decode Deployment status", err)
			}
		}
		if len(row.Spec) > 0 {
			if err := deployment.UnmarshalSpec(row.Spec); err != nil {
				return nil, huma.Error500InternalServerError("decode Deployment spec", err)
			}
		}

		// Cap unbounded backlogs (tailLines=0 means "all available") at a
		// fixed ceiling so a noisy adapter can't blow the response up to
		// arbitrary size. Adapters that respect TailLines stop emitting
		// past the cap; ones that don't get drained up to the cap and
		// then we close the loop on our side.
		tailLines := in.TailLines
		if tailLines <= 0 || tailLines > maxLogLines {
			tailLines = maxLogLines
		}
		ch, err := cfg.Coordinator.Logs(ctx, deployment, types.LogsInput{
			Follow:    false, // gated above; non-follow only for now
			TailLines: tailLines,
		})
		if err != nil {
			return nil, huma.Error502BadGateway("adapter logs: " + err.Error())
		}
		out := &deploymentLogsOutput{}
		out.Body.Lines = []deploymentLogLine{}
		for line := range ch {
			out.Body.Lines = append(out.Body.Lines, deploymentLogLine{
				Timestamp: line.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
				Stream:    line.Stream,
				Line:      line.Line,
			})
			if len(out.Body.Lines) >= maxLogLines {
				break
			}
		}
		return out, nil
	})
}
