package manifest

import (
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// ServerJSONToV1Alpha1Spec projects an upstream apiv0.ServerJSON onto our
// v1alpha1 MCPServerSpec. This remains a CLI-only bridge for imperative
// registry fetch paths until they are fully retired.
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
		for _, pkg := range server.Packages {
			spec.Packages = append(spec.Packages, packageToV1Alpha1(pkg))
		}
	}
	if len(server.Remotes) > 0 {
		spec.Remotes = make([]v1alpha1.MCPTransport, 0, len(server.Remotes))
		for _, remote := range server.Remotes {
			spec.Remotes = append(spec.Remotes, transportToV1Alpha1(remote))
		}
	}
	return spec
}

func PackageToV1Alpha1(pkg model.Package) v1alpha1.MCPPackage {
	return packageToV1Alpha1(pkg)
}

func packageToV1Alpha1(pkg model.Package) v1alpha1.MCPPackage {
	return v1alpha1.MCPPackage{
		RegistryType:         string(pkg.RegistryType),
		RegistryBaseURL:      pkg.RegistryBaseURL,
		Identifier:           pkg.Identifier,
		Version:              pkg.Version,
		FileSHA256:           pkg.FileSHA256,
		RuntimeHint:          pkg.RunTimeHint,
		Transport:            transportToV1Alpha1(pkg.Transport),
		RuntimeArguments:     argsToV1Alpha1(pkg.RuntimeArguments),
		PackageArguments:     argsToV1Alpha1(pkg.PackageArguments),
		EnvironmentVariables: kvsToV1Alpha1(pkg.EnvironmentVariables),
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
	for _, arg := range args {
		out = append(out, v1alpha1.MCPArgument{
			Type:        string(arg.Type),
			Name:        arg.Name,
			ValueHint:   arg.ValueHint,
			IsRepeated:  arg.IsRepeated,
			Description: arg.Description,
			IsRequired:  arg.IsRequired,
			Format:      string(arg.Format),
			Value:       arg.Value,
			IsSecret:    arg.IsSecret,
			Default:     arg.Default,
			Placeholder: arg.Placeholder,
			Choices:     arg.Choices,
			Variables:   variablesToV1Alpha1(arg.Variables),
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
	for name, variable := range in {
		out[name] = v1alpha1.MCPInputVariable{
			Description: variable.Description,
			IsRequired:  variable.IsRequired,
			Format:      string(variable.Format),
			Value:       variable.Value,
			IsSecret:    variable.IsSecret,
			Default:     variable.Default,
			Placeholder: variable.Placeholder,
			Choices:     variable.Choices,
		}
	}
	return out
}
