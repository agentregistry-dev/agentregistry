package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveResourceRef(t *testing.T) {
	unselected := namespaceSelection{Namespace: "default"}
	selectedDefault := namespaceSelection{Namespace: "default", Selected: true}
	teamA := namespaceSelection{Namespace: "team-a", Selected: true}

	tests := []struct {
		name    string
		arg     string
		sel     namespaceSelection
		want    resourceLookupRef
		wantErr string
	}{
		{name: "NAME takes the default", arg: "acme", sel: unselected, want: resourceLookupRef{Namespace: "default", Name: "acme"}},
		{name: "NAME takes the selection", arg: "acme", sel: teamA, want: resourceLookupRef{Namespace: "team-a", Name: "acme"}},
		{name: "NAMESPACE/NAME overrides an unselected default", arg: "team-b/acme", sel: unselected, want: resourceLookupRef{Namespace: "team-b", Name: "acme"}},
		{name: "NAMESPACE/NAME matching the selection", arg: "team-a/acme", sel: teamA, want: resourceLookupRef{Namespace: "team-a", Name: "acme"}},
		{name: "NAMESPACE/NAME conflicting with the selection", arg: "team-b/acme", sel: teamA, wantErr: `namespace "team-b" in "team-b/acme" conflicts with selected namespace "team-a"`},
		{name: "explicitly selected default conflicts too", arg: "team-b/acme", sel: selectedDefault, wantErr: "conflicts with selected namespace"},
		{name: "all is not a namespace", arg: "all/acme", sel: unselected, wantErr: `"all" in "all/acme" is not a namespace`},
		{name: "empty", arg: "", sel: unselected, wantErr: "must be NAME or NAMESPACE/NAME"},
		{name: "empty namespace", arg: "/acme", sel: unselected, wantErr: "must be NAME or NAMESPACE/NAME"},
		{name: "empty name", arg: "team-a/", sel: unselected, wantErr: "must be NAME or NAMESPACE/NAME"},
		{name: "extra segment", arg: "team-a/acme/x", sel: unselected, wantErr: "must be NAME or NAMESPACE/NAME"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveResourceRef(tt.arg, tt.sel)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResourceLookupRefString(t *testing.T) {
	tests := []struct {
		ref  resourceLookupRef
		want string
	}{
		{ref: resourceLookupRef{Namespace: "default", Name: "acme"}, want: "acme"},
		{ref: resourceLookupRef{Name: "acme"}, want: "acme"},
		{ref: resourceLookupRef{Namespace: "team-a", Name: "acme"}, want: "team-a/acme"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.ref.String())
	}
}
