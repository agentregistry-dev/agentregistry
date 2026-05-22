//go:build integration

package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

func TestSourceIndexProjectsRegisteredKindsWithoutPerKindBoilerplate(t *testing.T) {
	ctx := context.Background()
	stores, _, _ := newControllerTestStores(t)

	_, err := stores[v1alpha1.KindSkill].Upsert(ctx, &v1alpha1.Skill{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "tooling"},
		Spec:     v1alpha1.SkillSpec{Title: "first"},
	})
	require.NoError(t, err)

	sources := NewSourceIndex(stores)
	require.NoError(t, sources.Refresh(ctx))

	rows := sources.ListKind(v1alpha1.KindSkill)
	require.Len(t, rows, 1)
	skill, ok := rows[0].Object.(*v1alpha1.Skill)
	require.True(t, ok)
	require.Equal(t, "first", skill.Spec.Title)
	require.True(t, sources.ResourceExists(v1alpha1.ResourceRef{
		Kind: v1alpha1.KindSkill,
		Name: "tooling",
	}, "default"))

	_, err = stores[v1alpha1.KindSkill].Upsert(ctx, &v1alpha1.Skill{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "tooling"},
		Spec:     v1alpha1.SkillSpec{Title: "second"},
	})
	require.NoError(t, err)
	require.NoError(t, sources.ApplyEvent(ctx, v1alpha1store.ControlPlaneEvent{
		Key: v1alpha1store.ResourceKey{
			Kind:      v1alpha1.KindSkill,
			Namespace: "default",
			Name:      "tooling",
			Tag:       v1alpha1store.DefaultTag(),
		},
		Operation: "update",
	}))

	rows = sources.ListKind(v1alpha1.KindSkill)
	require.Len(t, rows, 1)
	skill, ok = rows[0].Object.(*v1alpha1.Skill)
	require.True(t, ok)
	require.Equal(t, "second", skill.Spec.Title)
}
