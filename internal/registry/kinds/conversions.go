// conversions.go provides typed conversion functions from declarative spec
// types to wire types. Used by server-side kind registrations in registry_app.go.
package kinds

import (
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/modelcontextprotocol/registry/pkg/model"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// ToAgentJSON converts a decoded AgentSpec + metadata into the wire AgentJSON.
func ToAgentJSON(md Metadata, spec *AgentSpec) *models.AgentJSON {
	aj := &models.AgentJSON{
		AgentManifest: models.AgentManifest{
			Name:              md.Name,
			Image:             spec.Image,
			Language:          spec.Language,
			Framework:         spec.Framework,
			ModelProvider:     spec.ModelProvider,
			ModelName:         spec.ModelName,
			Description:       spec.Description,
			Version:           md.Version,
			TelemetryEndpoint: spec.TelemetryEndpoint,
		},
		Title:      spec.Title,
		Version:    md.Version,
		Status:     "active",
		WebsiteURL: spec.WebsiteURL,
	}

	for _, s := range spec.McpServers {
		aj.McpServers = append(aj.McpServers, models.McpServerType{
			Type:                       s.Type,
			Name:                       s.Name,
			Image:                      s.Image,
			Build:                      s.Build,
			Command:                    s.Command,
			Args:                       s.Args,
			Env:                        s.Env,
			URL:                        s.URL,
			Headers:                    s.Headers,
			RegistryURL:                s.RegistryURL,
			RegistryServerName:         s.RegistryServerName,
			RegistryServerVersion:      s.RegistryServerVersion,
			RegistryServerPreferRemote: s.RegistryServerPreferRemote,
		})
	}
	for _, s := range spec.Skills {
		aj.Skills = append(aj.Skills, models.SkillRef{
			Name:                 s.Name,
			Image:                s.Image,
			RegistryURL:          s.RegistryURL,
			RegistrySkillName:    s.RegistrySkillName,
			RegistrySkillVersion: s.RegistrySkillVersion,
		})
	}
	for _, p := range spec.Prompts {
		aj.Prompts = append(aj.Prompts, models.PromptRef{
			Name:                  p.Name,
			RegistryURL:           p.RegistryURL,
			RegistryPromptName:    p.RegistryPromptName,
			RegistryPromptVersion: p.RegistryPromptVersion,
		})
	}
	if spec.Repository != nil {
		aj.Repository = &model.Repository{
			URL:       spec.Repository.URL,
			Source:    spec.Repository.Source,
			ID:        spec.Repository.ID,
			Subfolder: spec.Repository.Subfolder,
		}
	}
	for _, pkg := range spec.Packages {
		aj.Packages = append(aj.Packages, models.AgentPackageInfo{
			RegistryType: pkg.RegistryType,
			Identifier:   pkg.Identifier,
			Version:      pkg.Version,
			Transport: struct {
				Type string `json:"type"`
			}{Type: pkg.Transport.Type},
		})
	}
	for _, r := range spec.Remotes {
		aj.Remotes = append(aj.Remotes, model.Transport{
			Type: r.Type,
			URL:  r.URL,
		})
	}
	return aj
}

// ToSkillJSON converts a decoded SkillSpec + metadata into the wire SkillJSON.
func ToSkillJSON(md Metadata, spec *SkillSpec) *models.SkillJSON {
	sj := &models.SkillJSON{
		Name:        md.Name,
		Version:     md.Version,
		Title:       spec.Title,
		Category:    spec.Category,
		Description: spec.Description,
		WebsiteURL:  spec.WebsiteURL,
		Status:      "active",
	}

	if spec.Repository != nil {
		sj.Repository = &models.SkillRepository{
			URL:    spec.Repository.URL,
			Source: spec.Repository.Source,
		}
	}
	for _, pkg := range spec.Packages {
		sj.Packages = append(sj.Packages, models.SkillPackageInfo{
			RegistryType: pkg.RegistryType,
			Identifier:   pkg.Identifier,
			Version:      pkg.Version,
			Transport: struct {
				Type string `json:"type"`
			}{Type: pkg.Transport.Type},
		})
	}
	for _, r := range spec.Remotes {
		sj.Remotes = append(sj.Remotes, models.SkillRemoteInfo{
			URL: r.URL,
		})
	}
	return sj
}

// ToPromptJSON converts a decoded PromptSpec + metadata into the wire PromptJSON.
func ToPromptJSON(md Metadata, spec *PromptSpec) *models.PromptJSON {
	return &models.PromptJSON{
		Name:        md.Name,
		Version:     md.Version,
		Description: spec.Description,
		Content:     spec.Content,
	}
}

// ToServerJSON converts a decoded MCPSpec + metadata into the wire ServerJSON.
func ToServerJSON(md Metadata, spec *MCPSpec) *apiv0.ServerJSON {
	sj := &apiv0.ServerJSON{
		Schema:      spec.Schema,
		Name:        md.Name,
		Version:     md.Version,
		Description: spec.Description,
		Title:       spec.Title,
		WebsiteURL:  spec.WebsiteURL,
	}

	if sj.Schema == "" {
		sj.Schema = model.CurrentSchemaURL
	}

	if spec.Repository != nil {
		sj.Repository = &model.Repository{
			URL:       spec.Repository.URL,
			Source:    spec.Repository.Source,
			ID:        spec.Repository.ID,
			Subfolder: spec.Repository.Subfolder,
		}
	}

	for _, ic := range spec.Icons {
		sj.Icons = append(sj.Icons, model.Icon{
			Src:      ic.Src,
			MimeType: ic.MimeType,
			Sizes:    ic.Sizes,
			Theme:    ic.Theme,
		})
	}

	for _, p := range spec.Packages {
		pkg := model.Package{
			RegistryType:    p.RegistryType,
			RegistryBaseURL: p.RegistryBaseURL,
			Identifier:      p.Identifier,
			Version:         p.Version,
			FileSHA256:      p.FileSHA256,
			RunTimeHint:     p.RunTimeHint,
			Transport: model.Transport{
				Type:    p.Transport.Type,
				URL:     p.Transport.URL,
				Headers: toModelKeyValueInputs(p.Transport.Headers),
			},
			RuntimeArguments:     toModelArguments(p.RuntimeArguments),
			PackageArguments:     toModelArguments(p.PackageArguments),
			EnvironmentVariables: toModelKeyValueInputs(p.EnvironmentVariables),
		}
		sj.Packages = append(sj.Packages, pkg)
	}

	for _, r := range spec.Remotes {
		sj.Remotes = append(sj.Remotes, model.Transport{
			Type:    r.Type,
			URL:     r.URL,
			Headers: toModelKeyValueInputs(r.Headers),
		})
	}

	return sj
}

func toModelKeyValueInputs(kvs []MCPKeyValueInput) []model.KeyValueInput {
	if len(kvs) == 0 {
		return nil
	}
	out := make([]model.KeyValueInput, len(kvs))
	for i, kv := range kvs {
		out[i] = model.KeyValueInput{
			Name: kv.Name,
			InputWithVariables: model.InputWithVariables{
				Input: model.Input{
					Description: kv.Description,
					IsRequired:  kv.IsRequired,
					Format:      model.Format(kv.Format),
					Value:       kv.Value,
					IsSecret:    kv.IsSecret,
					Default:     kv.Default,
					Placeholder: kv.Placeholder,
					Choices:     kv.Choices,
				},
				Variables: toModelInputVariables(kv.Variables),
			},
		}
	}
	return out
}

func toModelArguments(args []MCPArgument) []model.Argument {
	if len(args) == 0 {
		return nil
	}
	out := make([]model.Argument, len(args))
	for i, a := range args {
		out[i] = model.Argument{
			Type:       model.ArgumentType(a.Type),
			Name:       a.Name,
			ValueHint:  a.ValueHint,
			IsRepeated: a.IsRepeated,
			InputWithVariables: model.InputWithVariables{
				Input: model.Input{
					Description: a.Description,
					IsRequired:  a.IsRequired,
					Format:      model.Format(a.Format),
					Value:       a.Value,
					IsSecret:    a.IsSecret,
					Default:     a.Default,
					Placeholder: a.Placeholder,
					Choices:     a.Choices,
				},
				Variables: toModelInputVariables(a.Variables),
			},
		}
	}
	return out
}

func toModelInputVariables(vars map[string]MCPInputVariable) map[string]model.Input {
	if len(vars) == 0 {
		return nil
	}
	out := make(map[string]model.Input, len(vars))
	for k, v := range vars {
		out[k] = model.Input{
			Description: v.Description,
			IsRequired:  v.IsRequired,
			Format:      model.Format(v.Format),
			Value:       v.Value,
			IsSecret:    v.IsSecret,
			Default:     v.Default,
			Placeholder: v.Placeholder,
			Choices:     v.Choices,
		}
	}
	return out
}
