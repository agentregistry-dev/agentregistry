package v1alpha1

import (
	"context"
	"fmt"
)

// Registry type identifiers for RegistryPackage.RegistryType. Values
// match the modelcontextprotocol/registry vocabulary — on-the-wire
// string literals, not an enum — so existing seed data and
// manifests round-trip unchanged.
const (
	RegistryTypeNPM   = "npm"
	RegistryTypePyPI  = "pypi"
	RegistryTypeOCI   = "oci"
	RegistryTypeNuGet = "nuget"
	RegistryTypeMCPB  = "mcpb"
)

// RegistryPackage is the minimal package view a registry validator
// consumes. AgentPackage, SkillPackage, and MCPPackage each expose
// these fields; the per-kind ValidateRegistries method converts its
// typed slice into this shape and calls the caller-supplied
// RegistryValidatorFunc.
//
// Extra fields present only on MCPPackage (RegistryBaseURL,
// FileSHA256) round-trip through here because the OCI validator
// rejects them — it needs to see them to reject them.
type RegistryPackage struct {
	RegistryType    string
	Identifier      string
	Version         string
	RegistryBaseURL string
	FileSHA256      string
}

// RegistryValidatorFunc validates a single package against its
// referenced external registry. Implementations fan out by
// RegistryType (OCI / npm / pypi / nuget / mcpb) to the
// appropriate per-registry validator. objectName is the resource's
// metadata.name, used for ownership annotations (e.g. OCI's
// io.modelcontextprotocol.server.name label match).
//
// A nil RegistryValidatorFunc is a no-op on the ValidateRegistries
// methods; callers that aren't wired with a dispatcher skip the
// check.
type RegistryValidatorFunc func(ctx context.Context, pkg RegistryPackage, objectName string) error

// validatePackages runs v against every element of packages,
// accumulating FieldErrors under the supplied path prefix (e.g.
// "spec.packages"). Returns nil FieldErrors when every validation
// passes — no-ops cleanly when v itself is nil.
func validatePackages(
	ctx context.Context,
	v RegistryValidatorFunc,
	packages []RegistryPackage,
	objectName, pathPrefix string,
) FieldErrors {
	if v == nil || len(packages) == 0 {
		return nil
	}
	var errs FieldErrors
	for i, pkg := range packages {
		if err := v(ctx, pkg, objectName); err != nil {
			errs.Append(fmt.Sprintf("%s[%d]", pathPrefix, i), err)
		}
	}
	return errs
}

// ValidateRegistries on *Agent converts AgentPackage entries to
// RegistryPackage and runs each through v.
func (a *Agent) ValidateRegistries(ctx context.Context, v RegistryValidatorFunc) error {
	if v == nil || len(a.Spec.Packages) == 0 {
		return nil
	}
	pkgs := make([]RegistryPackage, len(a.Spec.Packages))
	for i, p := range a.Spec.Packages {
		pkgs[i] = RegistryPackage{
			RegistryType: p.RegistryType,
			Identifier:   p.Identifier,
			Version:      p.Version,
		}
	}
	errs := validatePackages(ctx, v, pkgs, a.Metadata.Name, "spec.packages")
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// ValidateRegistries on *MCPServer converts MCPPackage entries
// (richer than AgentPackage: includes RegistryBaseURL + FileSHA256).
func (m *MCPServer) ValidateRegistries(ctx context.Context, v RegistryValidatorFunc) error {
	if v == nil || len(m.Spec.Packages) == 0 {
		return nil
	}
	pkgs := make([]RegistryPackage, len(m.Spec.Packages))
	for i, p := range m.Spec.Packages {
		pkgs[i] = RegistryPackage{
			RegistryType:    p.RegistryType,
			Identifier:      p.Identifier,
			Version:         p.Version,
			RegistryBaseURL: p.RegistryBaseURL,
			FileSHA256:      p.FileSHA256,
		}
	}
	errs := validatePackages(ctx, v, pkgs, m.Metadata.Name, "spec.packages")
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// ValidateRegistries on *Skill converts SkillPackage entries.
func (s *Skill) ValidateRegistries(ctx context.Context, v RegistryValidatorFunc) error {
	if v == nil || len(s.Spec.Packages) == 0 {
		return nil
	}
	pkgs := make([]RegistryPackage, len(s.Spec.Packages))
	for i, p := range s.Spec.Packages {
		pkgs[i] = RegistryPackage{
			RegistryType: p.RegistryType,
			Identifier:   p.Identifier,
			Version:      p.Version,
		}
	}
	errs := validatePackages(ctx, v, pkgs, s.Metadata.Name, "spec.packages")
	if len(errs) == 0 {
		return nil
	}
	return errs
}
