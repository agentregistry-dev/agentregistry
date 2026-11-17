// Package secret provides payload storage and redacted Secret resolution.
package secret

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

type StoreType string

const (
	StoreTypeDatabase   StoreType = "Database"
	StoreTypeKubernetes StoreType = "Kubernetes"
)

var (
	ErrNotFound        = errors.New("secret not found")
	ErrPayloadNotFound = errors.New("secret payload not found")
)

// Store persists Secret payload bytes. Implementations own at-rest protection.
type Store interface {
	Put(ctx context.Context, namespace, name string, data map[string][]byte) error
	Get(ctx context.Context, namespace, name string) (map[string][]byte, error)
	Delete(ctx context.Context, namespace, name string) error
}

// KeyProvider supplies encryption keys without coupling stores to configuration.
type KeyProvider interface {
	Key(ctx context.Context) ([]byte, error)
}

type StaticKeyProvider struct{ key []byte }

// NewStaticKeyProvider copies key so callers cannot mutate store key material.
func NewStaticKeyProvider(key []byte) *StaticKeyProvider {
	return &StaticKeyProvider{key: append([]byte(nil), key...)}
}

func (p *StaticKeyProvider) Key(context.Context) ([]byte, error) {
	if len(p.key) == 0 {
		return nil, errors.New("secret store encryption key not configured")
	}
	return append([]byte(nil), p.key...), nil
}

// LoadEncryptionKey decodes a hex-encoded AES-256 key.
func LoadEncryptionKey(value string) ([]byte, error) {
	key, err := hex.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("encryption key must be hex-encoded: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
	}
	return key, nil
}

// ObjectName keeps Kubernetes payload identities stable across implementations.
func ObjectName(namespace, name string) string {
	if namespace == "" {
		namespace = v1alpha1.DefaultNamespace
	}
	sum := sha256.Sum256([]byte(namespace + "/" + name))
	return "ar-" + hex.EncodeToString(sum[:16])
}

// Resolver is the trusted in-process path for reading Secret payloads.
type Resolver interface {
	Resolve(ctx context.Context, ref v1alpha1.SecretRef) (SensitiveValue, error)
	ResolveAll(ctx context.Context, ref v1alpha1.SecretRef) (map[string]SensitiveValue, error)
}

// Service resolves payloads from the configured Store.
type Service struct{ store Store }

func NewService(store Store) *Service { return &Service{store: store} }

func (s *Service) PutPayload(ctx context.Context, namespace, name string, data map[string][]byte) error {
	return s.store.Put(ctx, namespaceOrDefault(namespace), name, data)
}

func (s *Service) DeletePayload(ctx context.Context, namespace, name string) error {
	return s.store.Delete(ctx, namespaceOrDefault(namespace), name)
}

func (s *Service) Resolve(ctx context.Context, ref v1alpha1.SecretRef) (SensitiveValue, error) {
	if ref.Key == "" {
		return SensitiveValue{}, fmt.Errorf("resolve secret %s: key is required", refLabel(ref))
	}
	data, err := s.load(ctx, ref)
	if err != nil {
		return SensitiveValue{}, err
	}
	value, ok := data[ref.Key]
	if !ok {
		return SensitiveValue{}, fmt.Errorf("resolve secret %s: key %q: %w", refLabel(ref), ref.Key, ErrNotFound)
	}
	return NewSensitiveValue(value), nil
}

func (s *Service) ResolveAll(ctx context.Context, ref v1alpha1.SecretRef) (map[string]SensitiveValue, error) {
	data, err := s.load(ctx, ref)
	if err != nil {
		return nil, err
	}
	out := make(map[string]SensitiveValue, len(data))
	for key, value := range data {
		out[key] = NewSensitiveValue(value)
	}
	return out, nil
}

func (s *Service) load(ctx context.Context, ref v1alpha1.SecretRef) (map[string][]byte, error) {
	data, err := s.store.Get(ctx, namespaceOrDefault(ref.Namespace), ref.Name)
	if err != nil {
		if errors.Is(err, ErrPayloadNotFound) {
			return nil, fmt.Errorf("resolve secret %s: %w", refLabel(ref), ErrNotFound)
		}
		return nil, fmt.Errorf("resolve secret %s: %w", refLabel(ref), err)
	}
	return data, nil
}

func namespaceOrDefault(namespace string) string {
	if namespace == "" {
		return v1alpha1.DefaultNamespace
	}
	return namespace
}

func refLabel(ref v1alpha1.SecretRef) string {
	return namespaceOrDefault(ref.Namespace) + "/" + ref.Name
}

var _ Resolver = (*Service)(nil)

const redacted = "[REDACTED]"

// SensitiveValue prevents accidental plaintext formatting or serialization.
type SensitiveValue struct{ raw []byte }

func NewSensitiveValue(value []byte) SensitiveValue {
	copyOfValue := make([]byte, len(value))
	copy(copyOfValue, value)
	return SensitiveValue{raw: copyOfValue}
}

// Reveal returns a copy of the plaintext and is the only plaintext accessor.
func (v SensitiveValue) Reveal() []byte {
	value := make([]byte, len(v.raw))
	copy(value, v.raw)
	return value
}

func (v SensitiveValue) Len() int { return len(v.raw) }

func (SensitiveValue) String() string { return redacted }

func (SensitiveValue) GoString() string { return redacted }

func (SensitiveValue) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte(redacted)) }

func (SensitiveValue) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }

func (SensitiveValue) MarshalText() ([]byte, error) { return []byte(redacted), nil }

func (SensitiveValue) LogValue() slog.Value { return slog.StringValue(redacted) }
