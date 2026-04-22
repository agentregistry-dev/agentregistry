package models

// v1alpha1_bridge.go projects the legacy upstream
// github.com/modelcontextprotocol/registry/pkg/api/v0 ServerJSON shape onto
// the v1alpha1 MCPServerSpec. Exists solely to let the imperative CLI
// (internal/cli/{agent,mcp}/*) keep working while the declarative-CLI
// replacement branch catches up — after that merge lands, this file, the
// pkg/models package, and every upstream-registry import disappear
// together. Production (platforms/utils, handlers, Store) speaks v1alpha1
// directly with no ServerJSON dependency.

import (
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// ServerJSONToV1Alpha1Spec projects an upstream apiv0.ServerJSON onto a
// v1alpha1.MCPServerSpec. Field-for-field translation; the underlying
// shapes are structurally similar by design (see MCPServerSpec doc
// comment). Returns a zero-value spec for a nil input.
func ServerJSONToV1Alpha1Spec(server *apiv0.ServerJSON) v1alpha1.MCPServerSpec {
	if server == nil {
		return v1alpha1.MCPServerSpec{}
	}
	spec := v1alpha1.MCPServerSpec{
		Title:       server.Title,
		Description: server.Description,
		WebsiteURL:  server.WebsiteURL,
	}
	if server.Repository != nil {
		spec.Repository = &v1alpha1.Repository{
			URL:       server.Repository.URL,
			Source:    server.Repository.Source,
			ID:        server.Repository.ID,
			Subfolder: server.Repository.Subfolder,
		}
	}
	if len(server.Packages) > 0 {
		spec.Packages = make([]v1alpha1.MCPPackage, 0, len(server.Packages))
		for _, p := range server.Packages {
			spec.Packages = append(spec.Packages, packageToV1Alpha1(p))
		}
	}
	if len(server.Remotes) > 0 {
		spec.Remotes = make([]v1alpha1.MCPTransport, 0, len(server.Remotes))
		for _, r := range server.Remotes {
			spec.Remotes = append(spec.Remotes, transportToV1Alpha1(r))
		}
	}
	return spec
}

// PackageToV1Alpha1 is the exported single-package variant of the bridge —
// CLI code sometimes needs just one package converted (e.g. Dockerfile
// build path detection).
func PackageToV1Alpha1(p model.Package) v1alpha1.MCPPackage {
	return packageToV1Alpha1(p)
}

func packageToV1Alpha1(p model.Package) v1alpha1.MCPPackage {
	return v1alpha1.MCPPackage{
		RegistryType:         string(p.RegistryType),
		RegistryBaseURL:      p.RegistryBaseURL,
		Identifier:           p.Identifier,
		Version:              p.Version,
		FileSHA256:           p.FileSHA256,
		RuntimeHint:          p.RunTimeHint,
		Transport:            transportToV1Alpha1(p.Transport),
		RuntimeArguments:     argsToV1Alpha1(p.RuntimeArguments),
		PackageArguments:     argsToV1Alpha1(p.PackageArguments),
		EnvironmentVariables: kvsToV1Alpha1(p.EnvironmentVariables),
	}
}

func transportToV1Alpha1(t model.Transport) v1alpha1.MCPTransport {
	return v1alpha1.MCPTransport{
		Type:    string(t.Type),
		URL:     t.URL,
		Headers: kvsToV1Alpha1(t.Headers),
	}
}

func argsToV1Alpha1(args []model.Argument) []v1alpha1.MCPArgument {
	if len(args) == 0 {
		return nil
	}
	out := make([]v1alpha1.MCPArgument, 0, len(args))
	for _, a := range args {
		out = append(out, v1alpha1.MCPArgument{
			Type:        string(a.Type),
			Name:        a.Name,
			ValueHint:   a.ValueHint,
			IsRepeated:  a.IsRepeated,
			Description: a.Description,
			IsRequired:  a.IsRequired,
			Format:      string(a.Format),
			Value:       a.Value,
			IsSecret:    a.IsSecret,
			Default:     a.Default,
			Placeholder: a.Placeholder,
			Choices:     a.Choices,
			Variables:   variablesToV1Alpha1(a.Variables),
		})
	}
	return out
}

func kvsToV1Alpha1(kvs []model.KeyValueInput) []v1alpha1.MCPKeyValueInput {
	if len(kvs) == 0 {
		return nil
	}
	out := make([]v1alpha1.MCPKeyValueInput, 0, len(kvs))
	for _, kv := range kvs {
		out = append(out, v1alpha1.MCPKeyValueInput{
			Name:        kv.Name,
			Description: kv.Description,
			IsRequired:  kv.IsRequired,
			Format:      string(kv.Format),
			Value:       kv.Value,
			IsSecret:    kv.IsSecret,
			Default:     kv.Default,
			Placeholder: kv.Placeholder,
			Choices:     kv.Choices,
			Variables:   variablesToV1Alpha1(kv.Variables),
		})
	}
	return out
}

func variablesToV1Alpha1(in map[string]model.Input) map[string]v1alpha1.MCPInputVariable {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]v1alpha1.MCPInputVariable, len(in))
	for k, v := range in {
		out[k] = v1alpha1.MCPInputVariable{
			Description: v.Description,
			IsRequired:  v.IsRequired,
			Format:      string(v.Format),
			Value:       v.Value,
			IsSecret:    v.IsSecret,
			Default:     v.Default,
			Placeholder: v.Placeholder,
			Choices:     v.Choices,
		}
	}
	return out
}
