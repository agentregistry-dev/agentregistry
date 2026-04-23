package v1alpha1

import "strings"

// Readme is an optional long-form documentation block attached to a
// registry artifact. The list APIs strip Content to keep collection
// responses lightweight; callers that need the full body can fetch the
// object directly or use the readme subresource route.
type Readme struct {
	ContentType string `json:"contentType,omitempty" yaml:"contentType,omitempty"`
	Encoding    string `json:"encoding,omitempty" yaml:"encoding,omitempty"`
	Source      string `json:"source,omitempty" yaml:"source,omitempty"`
	Content     string `json:"content,omitempty" yaml:"content,omitempty"`
}

// HasContent reports whether the readme carries a non-empty body.
func (r *Readme) HasContent() bool {
	return r != nil && strings.TrimSpace(r.Content) != ""
}

// ObjectReadme returns the optional readme block carried by an object's spec.
// Kinds without README support return nil.
func ObjectReadme(obj Object) *Readme {
	switch o := obj.(type) {
	case *Agent:
		return o.Spec.Readme
	case *MCPServer:
		return o.Spec.Readme
	case *Skill:
		return o.Spec.Readme
	case *Prompt:
		return o.Spec.Readme
	default:
		return nil
	}
}

// StripObjectReadmeContent removes the heavy readme body from an object while
// preserving the rest of the metadata block.
func StripObjectReadmeContent(obj Object) {
	if readme := ObjectReadme(obj); readme != nil {
		readme.Content = ""
	}
}
