package resource

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/client"
)

// ResourceHandler translates between declarative YAML Resources and the registry API.
type ResourceHandler interface {
	Kind() string
	Singular() string
	Plural() string
	Apply(c *client.Client, r *scheme.Resource) error
	List(c *client.Client) ([]any, error)
	Get(c *client.Client, name string) (any, error)
	Delete(c *client.Client, name, version string) error
	TableColumns() []string
	TableRow(item any) []string
	ToResource(item any) *scheme.Resource
}

// Registry maps kind names and aliases to ResourceHandler implementations.
type Registry struct {
	mu      sync.RWMutex
	byKind  map[string]ResourceHandler
	aliases map[string]string
}

func NewRegistry() *Registry {
	return &Registry{
		byKind:  make(map[string]ResourceHandler),
		aliases: make(map[string]string),
	}
}

func (reg *Registry) Register(h ResourceHandler) {
	reg.mu.Lock()
	defer reg.mu.Unlock()

	kind := h.Kind()
	if _, exists := reg.byKind[kind]; exists {
		panic(fmt.Sprintf("resource: duplicate registration for kind %q", kind))
	}
	reg.byKind[kind] = h
	reg.aliases[strings.ToLower(kind)] = kind
	reg.aliases[strings.ToLower(h.Singular())] = kind
	reg.aliases[strings.ToLower(h.Plural())] = kind
}

func (reg *Registry) Lookup(name string) (ResourceHandler, error) {
	reg.mu.RLock()
	defer reg.mu.RUnlock()

	lower := strings.ToLower(name)
	if kind, ok := reg.aliases[lower]; ok {
		return reg.byKind[kind], nil
	}
	return nil, fmt.Errorf("unknown resource type %q; supported types: %s", name, reg.knownTypes())
}

// All returns all registered handlers in a stable display order.
func (reg *Registry) All() []ResourceHandler {
	reg.mu.RLock()
	defer reg.mu.RUnlock()

	// Fixed display order: agents, mcps, skills, prompts
	order := []string{"Agent", "MCPServer", "Skill", "Prompt"}
	var out []ResourceHandler
	for _, kind := range order {
		if h, ok := reg.byKind[kind]; ok {
			out = append(out, h)
		}
	}
	// Append any kinds not in the fixed order, sorted for deterministic output.
	var extra []string
	for kind := range reg.byKind {
		if !slices.Contains(order, kind) {
			extra = append(extra, kind)
		}
	}
	slices.Sort(extra)
	for _, kind := range extra {
		out = append(out, reg.byKind[kind])
	}
	return out
}

func (reg *Registry) knownTypes() string {
	var names []string
	for _, h := range reg.byKind {
		names = append(names, h.Plural())
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

// DefaultRegistry is the global registry used by CLI commands.
var DefaultRegistry = NewRegistry()

func Register(h ResourceHandler) {
	DefaultRegistry.Register(h)
}

func Lookup(name string) (ResourceHandler, error) {
	return DefaultRegistry.Lookup(name)
}

func All() []ResourceHandler {
	return DefaultRegistry.All()
}
