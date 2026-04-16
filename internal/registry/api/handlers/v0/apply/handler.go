// Package apply implements the batch POST /v0/apply and DELETE /v0/apply
// endpoints. These are the primary surface for declarative resource management.
package apply

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/common"
	"github.com/agentregistry-dev/agentregistry/internal/registry/kinds"
	"github.com/danielgtaylor/huma/v2"
	"gopkg.in/yaml.v3"
)

// ApplyInput is the Huma input for both POST and DELETE /v0/apply.
// RawBody is used so the handler can accept multi-document YAML directly.
type ApplyInput struct {
	Force   bool   `query:"force" doc:"Force overwrite even when the resource has drifted"`
	DryRun  bool   `query:"dryRun" doc:"Validate without persisting changes"`
	RawBody []byte `contentType:"application/yaml" required:"true" doc:"Multi-document YAML or JSON array of resource documents (JSON is also accepted because it is valid YAML)"`
}

// ApplyOutput is the response envelope.
type ApplyOutput struct {
	Body struct {
		Results []kinds.Result `json:"results"`
	}
}

type op int

const (
	opApply op = iota
	opDelete
)

// RegisterApplyEndpoints registers POST /v0/apply and DELETE /v0/apply.
func RegisterApplyEndpoints(api huma.API, pathPrefix string, reg *kinds.Registry) {
	huma.Register(api, huma.Operation{
		OperationID: "apply",
		Method:      http.MethodPost,
		Path:        pathPrefix + "/apply",
		Summary:     "Apply one or more resources",
		Tags:        []string{"apply"},
	}, func(ctx context.Context, input *ApplyInput) (*ApplyOutput, error) {
		return applyBatch(ctx, reg, input, opApply)
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-apply",
		Method:      http.MethodDelete,
		Path:        pathPrefix + "/apply",
		Summary:     "Delete one or more resources by YAML document",
		Tags:        []string{"apply"},
	}, func(ctx context.Context, input *ApplyInput) (*ApplyOutput, error) {
		return applyBatch(ctx, reg, input, opDelete)
	})
}

// rawEnvelope is used for the first-pass YAML decode to extract kind/metadata
// before dispatching to the registry's full Decode.
type rawEnvelope struct {
	Kind     string         `yaml:"kind"`
	Metadata kinds.Metadata `yaml:"metadata"`
}

func applyBatch(ctx context.Context, reg *kinds.Registry, input *ApplyInput, mode op) (*ApplyOutput, error) {
	docs, perDocErrors, err := decodeMultiPermissive(reg, input.RawBody)
	if err != nil {
		return nil, huma.Error400BadRequest("parsing body: " + err.Error())
	}

	opts := kinds.ApplyOpts{Force: input.Force, DryRun: input.DryRun}
	results := make([]kinds.Result, 0, len(docs)+len(perDocErrors))

	// First, collect per-doc decode errors (unknown kinds, bad spec, etc.)
	results = append(results, perDocErrors...)

	// Then dispatch each successfully decoded doc.
	for _, doc := range docs {
		res := dispatchOne(ctx, reg, doc, opts, mode)
		results = append(results, res)
	}

	out := &ApplyOutput{}
	out.Body.Results = results
	return out, nil
}

// decodeMultiPermissive splits a multi-document YAML stream and decodes each
// document. Unlike Registry.DecodeMulti, it does NOT fail the entire batch on
// per-document errors (unknown kind, bad spec). Instead it returns successfully
// decoded docs and a slice of Result for each failed doc.
func decodeMultiPermissive(reg *kinds.Registry, raw []byte) ([]*kinds.Document, []kinds.Result, error) {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	var docs []*kinds.Document
	var errResults []kinds.Result

	for {
		var node yaml.Node
		err := dec.Decode(&node)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// Truly malformed YAML — can't continue parsing.
			return nil, nil, err
		}
		if node.Kind == 0 {
			continue
		}

		// Re-encode the node so we can pass it to the registry's Decode.
		var buf bytes.Buffer
		enc := yaml.NewEncoder(&buf)
		if err := enc.Encode(&node); err != nil {
			_ = enc.Close()
			return nil, nil, err
		}
		_ = enc.Close()

		doc, err := reg.Decode(buf.Bytes())
		if err != nil {
			// Per-doc error: extract kind+name from a lightweight parse.
			var env rawEnvelope
			_ = yaml.Unmarshal(buf.Bytes(), &env)
			errResults = append(errResults, kinds.Result{
				Kind:    env.Kind,
				Name:    env.Metadata.Name,
				Version: env.Metadata.Version,
				Status:  kinds.StatusFailed,
				Error:   err.Error(),
			})
			continue
		}
		docs = append(docs, doc)
	}
	return docs, errResults, nil
}

func dispatchOne(ctx context.Context, reg *kinds.Registry, doc *kinds.Document, opts kinds.ApplyOpts, mode op) kinds.Result {
	k, err := reg.Lookup(doc.Kind)
	if err != nil {
		return kinds.Result{
			Kind: doc.Kind, Name: doc.Metadata.Name, Version: doc.Metadata.Version,
			Status: kinds.StatusFailed, Error: err.Error(),
		}
	}

	switch mode {
	case opApply:
		if k.Apply == nil {
			return kinds.Result{
				Kind: k.Kind, Name: doc.Metadata.Name, Version: doc.Metadata.Version,
				Status: kinds.StatusFailed, Error: "kind does not support apply",
			}
		}
		res, err := k.Apply(ctx, doc, opts)
		if err != nil {
			status, msg := common.ClassifyApplyError(err)
			return kinds.Result{
				Kind: k.Kind, Name: doc.Metadata.Name, Version: doc.Metadata.Version,
				Status: status, Error: msg,
			}
		}
		return *res

	case opDelete:
		if k.Delete == nil {
			return kinds.Result{
				Kind: k.Kind, Name: doc.Metadata.Name, Version: doc.Metadata.Version,
				Status: kinds.StatusFailed, Error: "kind does not support delete",
			}
		}
		if err := k.Delete(ctx, doc.Metadata.Name, doc.Metadata.Version); err != nil {
			status, msg := common.ClassifyApplyError(err)
			return kinds.Result{
				Kind: k.Kind, Name: doc.Metadata.Name, Version: doc.Metadata.Version,
				Status: status, Error: msg,
			}
		}
		return kinds.Result{
			Kind: k.Kind, Name: doc.Metadata.Name, Version: doc.Metadata.Version,
			Status: kinds.StatusDeleted,
		}
	}

	// Unreachable.
	return kinds.Result{Status: kinds.StatusFailed, Error: "unknown operation mode"}
}
