package kagent

import (
	"context"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

type fakeClient struct {
	agents      map[string]*agentPayload
	toolServers map[string]*toolServerSpec
	errs        map[string]error
	deleted     []string
}

func newFakeClient() *fakeClient {
	return &fakeClient{
		agents:      map[string]*agentPayload{},
		toolServers: map[string]*toolServerSpec{},
		errs:        map[string]error{},
	}
}

func key(ns, name string) string { return ns + "/" + name }

func (f *fakeClient) ensureAgent(_ context.Context, a *agentPayload) error {
	if err := f.errs["ensureAgent:"+key(a.Namespace, a.Name)]; err != nil {
		return err
	}
	f.agents[key(a.Namespace, a.Name)] = a
	return nil
}

func (f *fakeClient) deleteAgent(_ context.Context, ns, name string) error {
	if err := f.errs["deleteAgent:"+key(ns, name)]; err != nil {
		return err
	}
	f.deleted = append(f.deleted, "Agent:"+key(ns, name))
	delete(f.agents, key(ns, name))
	return nil
}

func (f *fakeClient) ensureToolServer(_ context.Context, ts *toolServerSpec) error {
	if err := f.errs["ensureToolServer:"+key(ts.Namespace(), ts.Name())]; err != nil {
		return err
	}
	f.toolServers[key(ts.Namespace(), ts.Name())] = ts
	return nil
}

func (f *fakeClient) deleteToolServer(_ context.Context, ns, name string) error {
	if err := f.errs["deleteToolServer:"+key(ns, name)]; err != nil {
		return err
	}
	f.deleted = append(f.deleted, "MCPServer:"+key(ns, name))
	delete(f.toolServers, key(ns, name))
	return nil
}

func (f *fakeClient) listAgents(context.Context) ([]remoteWorkload, error) {
	var out []remoteWorkload
	for k := range f.agents {
		ns, name := splitRef(k)
		out = append(out, remoteWorkload{Kind: v1alpha1.KindAgent, Namespace: ns, Name: name})
	}
	return out, nil
}

func (f *fakeClient) listToolServers(context.Context) ([]remoteWorkload, error) {
	var out []remoteWorkload
	for k := range f.toolServers {
		ns, name := splitRef(k)
		out = append(out, remoteWorkload{Kind: v1alpha1.KindMCPServer, Namespace: ns, Name: name})
	}
	return out, nil
}

var _ kagentClient = (*fakeClient)(nil)
