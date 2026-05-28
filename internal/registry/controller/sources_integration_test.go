//go:build integration

package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

func TestSourceIndexProjectsTypedBuiltInCollections(t *testing.T) {
	ctx := context.Background()
	stores, _, _ := newControllerTestStores(t)

	_, err := stores[v1alpha1.KindSkill].Upsert(ctx, &v1alpha1.Skill{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "tooling"},
		Spec:     v1alpha1.SkillSpec{Title: "first"},
	})
	require.NoError(t, err)

	sources := NewSourceIndex(stores)
	require.NoError(t, sources.Refresh(ctx))

	rows := sources.Skills.List()
	require.Len(t, rows, 1)
	require.Equal(t, "first", rows[0].Skill.Spec.Title)
	skillKey := v1alpha1store.ResourceKey{
		Kind:      v1alpha1.KindSkill,
		Namespace: "default",
		Name:      "tooling",
		Tag:       v1alpha1store.DefaultTag(),
	}
	require.NotNil(t, sources.Skills.GetKey(sourceObjectKey(skillKey)))

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

	rows = sources.Skills.List()
	require.Len(t, rows, 1)
	require.Equal(t, "second", rows[0].Skill.Spec.Title)
}
