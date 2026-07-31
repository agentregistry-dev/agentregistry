package auth_test

import (
	"context"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
)

func TestPublicAuthzProvider(t *testing.T) {
	provider := auth.NewPublicAuthzProvider()
	resource := auth.Resource{Type: auth.PermissionArtifactTypeAgent, Name: "test-agent"}

	for _, action := range []auth.PermissionAction{
		auth.PermissionActionRead,
		auth.PermissionActionPublish,
		auth.PermissionActionEdit,
		auth.PermissionActionDelete,
		auth.PermissionActionDeploy,
	} {
		t.Run(string(action), func(t *testing.T) {
			if err := provider.Check(context.Background(), nil, action, resource); err != nil {
				t.Fatalf("Check() error = %v, want nil", err)
			}
		})
	}

	if !provider.IsRegistryAdmin(context.Background(), nil) {
		t.Fatal("IsRegistryAdmin() = false, want true")
	}
}
