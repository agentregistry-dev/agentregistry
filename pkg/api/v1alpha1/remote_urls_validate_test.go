package v1alpha1_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	v1 "github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// fakeChecker records every invocation and returns conflictErr for URLs
// in conflict. Lets tests assert which (kind, url, excludeName) tuples
// were queried.
type fakeChecker struct {
	conflict    map[string]bool // url → true means conflict
	conflictErr error
	calls       []fakeCall
}

type fakeCall struct {
	kind        string
	namespace   string
	url         string
	excludeName string
}

func (f *fakeChecker) Check(ctx context.Context, kind, namespace, url, excludeName string) error {
	f.calls = append(f.calls, fakeCall{kind, namespace, url, excludeName})
	if f.conflict[url] {
		return f.conflictErr
	}
	return nil
}

var errRemoteConflict = errors.New("url in use")

func TestAgent_ValidateUniqueRemoteURLs_NilCheckerIsNoOp(t *testing.T) {
	a := &v1.Agent{
		Metadata: v1.ObjectMeta{Namespace: "default", Name: "payments", Version: "v1"},
		Spec: v1.AgentSpec{
			Remotes: []v1.AgentRemote{{Type: "sse", URL: "https://api.example.com/x"}},
		},
	}
	require.NoError(t, a.ValidateUniqueRemoteURLs(context.Background(), nil))
}

func TestAgent_ValidateUniqueRemoteURLs_NoRemotesIsNoOp(t *testing.T) {
	fc := &fakeChecker{}
	a := &v1.Agent{
		Metadata: v1.ObjectMeta{Namespace: "default", Name: "payments", Version: "v1"},
	}
	require.NoError(t, a.ValidateUniqueRemoteURLs(context.Background(), fc.Check))
	require.Empty(t, fc.calls)
}

func TestAgent_ValidateUniqueRemoteURLs_PassesWhenNoConflict(t *testing.T) {
	fc := &fakeChecker{}
	a := &v1.Agent{
		Metadata: v1.ObjectMeta{Namespace: "default", Name: "payments", Version: "v1"},
		Spec: v1.AgentSpec{
			Remotes: []v1.AgentRemote{
				{Type: "sse", URL: "https://api.example.com/a"},
				{Type: "sse", URL: "https://api.example.com/b"},
			},
		},
	}
	require.NoError(t, a.ValidateUniqueRemoteURLs(context.Background(), fc.Check))
	require.Len(t, fc.calls, 2)
	require.Equal(t, v1.KindAgent, fc.calls[0].kind)
	require.Equal(t, "default", fc.calls[0].namespace)
	require.Equal(t, "payments", fc.calls[0].excludeName)
}

func TestAgent_ValidateUniqueRemoteURLs_ReportsConflicts(t *testing.T) {
	fc := &fakeChecker{
		conflict:    map[string]bool{"https://api.example.com/b": true},
		conflictErr: errRemoteConflict,
	}
	a := &v1.Agent{
		Metadata: v1.ObjectMeta{Namespace: "default", Name: "payments", Version: "v1"},
		Spec: v1.AgentSpec{
			Remotes: []v1.AgentRemote{
				{Type: "sse", URL: "https://api.example.com/a"},
				{Type: "sse", URL: "https://api.example.com/b"},
			},
		},
	}
	err := a.ValidateUniqueRemoteURLs(context.Background(), fc.Check)
	require.Error(t, err)
	var errs v1.FieldErrors
	require.ErrorAs(t, err, &errs)
	require.Len(t, errs, 1)
	require.Equal(t, "spec.remotes[1].url", errs[0].Path)
	require.ErrorIs(t, errs[0].Cause, errRemoteConflict)
}

func TestAgent_ValidateUniqueRemoteURLs_SkipsEmptyURLs(t *testing.T) {
	fc := &fakeChecker{}
	a := &v1.Agent{
		Metadata: v1.ObjectMeta{Namespace: "default", Name: "payments", Version: "v1"},
		Spec: v1.AgentSpec{
			Remotes: []v1.AgentRemote{
				{Type: "sse", URL: ""},
				{Type: "sse", URL: "https://api.example.com/a"},
			},
		},
	}
	require.NoError(t, a.ValidateUniqueRemoteURLs(context.Background(), fc.Check))
	require.Len(t, fc.calls, 1)
	require.Equal(t, "https://api.example.com/a", fc.calls[0].url)
}

func TestMCPServer_ValidateUniqueRemoteURLs_UsesMCPServerKind(t *testing.T) {
	fc := &fakeChecker{}
	m := &v1.MCPServer{
		Metadata: v1.ObjectMeta{Namespace: "prod", Name: "tools", Version: "v1"},
		Spec: v1.MCPServerSpec{
			Remotes: []v1.MCPTransport{{Type: "streamable-http", URL: "https://mcp.example.com"}},
		},
	}
	require.NoError(t, m.ValidateUniqueRemoteURLs(context.Background(), fc.Check))
	require.Len(t, fc.calls, 1)
	require.Equal(t, v1.KindMCPServer, fc.calls[0].kind)
	require.Equal(t, "prod", fc.calls[0].namespace)
	require.Equal(t, "tools", fc.calls[0].excludeName)
}

func TestSkill_ValidateUniqueRemoteURLs_UsesSkillKind(t *testing.T) {
	fc := &fakeChecker{}
	s := &v1.Skill{
		Metadata: v1.ObjectMeta{Namespace: "default", Name: "doc-search", Version: "v1"},
		Spec: v1.SkillSpec{
			Remotes: []v1.SkillRemote{{URL: "https://skills.example.com/doc"}},
		},
	}
	require.NoError(t, s.ValidateUniqueRemoteURLs(context.Background(), fc.Check))
	require.Len(t, fc.calls, 1)
	require.Equal(t, v1.KindSkill, fc.calls[0].kind)
}

func TestKindsWithoutRemotes_AreNoOps(t *testing.T) {
	fc := &fakeChecker{
		conflict:    map[string]bool{"https://anything": true},
		conflictErr: errRemoteConflict,
	}
	ctx := context.Background()

	p := &v1.Prompt{Metadata: v1.ObjectMeta{Namespace: "default", Name: "p", Version: "v1"}}
	require.NoError(t, p.ValidateUniqueRemoteURLs(ctx, fc.Check))

	prov := &v1.Provider{Metadata: v1.ObjectMeta{Namespace: "default", Name: "local", Version: "v1"}}
	require.NoError(t, prov.ValidateUniqueRemoteURLs(ctx, fc.Check))

	d := &v1.Deployment{Metadata: v1.ObjectMeta{Namespace: "default", Name: "d", Version: "v1"}}
	require.NoError(t, d.ValidateUniqueRemoteURLs(ctx, fc.Check))

	require.Empty(t, fc.calls, "kinds without Remotes should not invoke the checker")
}
