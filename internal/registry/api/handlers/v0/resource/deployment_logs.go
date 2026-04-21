package resource

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// DeploymentLogsConfig bundles the inputs for RegisterDeploymentLogs. The
// coordinator drives adapter.Logs; the store fetches the Deployment row so
// the endpoint can reject 404s early.
type DeploymentLogsConfig struct {
	BasePrefix  string
	Store       *database.Store
	Coordinator *deploymentsvc.V1Alpha1Coordinator
}

// deploymentLogsInput is the request body — query flags for the stream +
// path segments for the deployment identity.
type deploymentLogsInput struct {
	Namespace string `path:"namespace"`
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

type deploymentLogsOutput struct {
	Body struct {
		Lines []deploymentLogLine `json:"lines"`
	}
}

// RegisterDeploymentLogs wires GET
// {basePrefix}/namespaces/{ns}/deployments/{name}/{version}/logs. The
// response is a JSON payload of log records drained from
// coordinator.Logs; follow=true keeps the channel open until the client
// disconnects (or until the adapter's context is cancelled).
//
// Non-streaming for now — huma lacks first-class SSE output and the
// kubernetes/local adapters still return closed channels. When real log
// streaming lands upstream, swap this for an SSE/chunked handler at the
// same path without touching the coordinator surface.
func RegisterDeploymentLogs(api huma.API, cfg DeploymentLogsConfig) {
	if cfg.Coordinator == nil || cfg.Store == nil {
		return
	}
	path := cfg.BasePrefix + "/namespaces/{namespace}/deployments/{name}/{version}/logs"

	huma.Register(api, huma.Operation{
		OperationID: "get-deployment-logs",
		Method:      http.MethodGet,
		Path:        path,
		Summary:     "Stream logs from a deployment's runtime workload",
	}, func(ctx context.Context, in *deploymentLogsInput) (*deploymentLogsOutput, error) {
		row, err := cfg.Store.Get(ctx, in.Namespace, in.Name, in.Version)
		if err != nil {
			if errors.Is(err, pkgdb.ErrNotFound) {
				return nil, huma.Error404NotFound(fmt.Sprintf("Deployment %q/%q@%q not found", in.Namespace, in.Name, in.Version))
			}
			return nil, huma.Error500InternalServerError("fetch Deployment", err)
		}
		deployment := &v1alpha1.Deployment{}
		deployment.SetTypeMeta(v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindDeployment})
		deployment.SetMetadata(row.Metadata)
		deployment.SetStatus(row.Status)
		if len(row.Spec) > 0 {
			if err := deployment.UnmarshalSpec(row.Spec); err != nil {
				return nil, huma.Error500InternalServerError("decode Deployment spec", err)
			}
		}

		ch, err := cfg.Coordinator.Logs(ctx, deployment, types.LogsInput{
			Follow:    in.Follow,
			TailLines: in.TailLines,
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
		}
		return out, nil
	})
}
