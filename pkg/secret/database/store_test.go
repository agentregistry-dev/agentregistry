package database

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/secret"
)

type memoryPersistence struct{ ciphertext []byte }

func (m *memoryPersistence) UpsertSecretPayload(_ context.Context, _, _ string, ciphertext []byte) error {
	m.ciphertext = append([]byte(nil), ciphertext...)
	return nil
}

func (m *memoryPersistence) GetSecretPayload(context.Context, string, string) ([]byte, error) {
	if m.ciphertext == nil {
		return nil, pkgdb.ErrNotFound
	}
	return append([]byte(nil), m.ciphertext...), nil
}

func (m *memoryPersistence) DeleteSecretPayload(context.Context, string, string) error {
	m.ciphertext = nil
	return nil
}

func TestStoreRoundTripAndRedaction(t *testing.T) {
	persistence := &memoryPersistence{}
	key := make([]byte, 32)
	store := New(persistence, secret.NewStaticKeyProvider(key))
	payload := map[string][]byte{"token": []byte("plaintext")}

	require.NoError(t, store.Put(t.Context(), "default", "creds", payload))
	require.False(t, bytes.Contains(persistence.ciphertext, payload["token"]))
	got, err := store.Get(t.Context(), "default", "creds")
	require.NoError(t, err)
	require.Equal(t, payload, got)

	require.NoError(t, store.Delete(t.Context(), "default", "creds"))
	_, err = store.Get(t.Context(), "default", "creds")
	require.ErrorIs(t, err, secret.ErrPayloadNotFound)
}

func TestStoreDoesNotLeakPlaintextOnDecryptFailure(t *testing.T) {
	persistence := &memoryPersistence{}
	good := New(persistence, secret.NewStaticKeyProvider(make([]byte, 32)))
	require.NoError(t, good.Put(t.Context(), "default", "creds", map[string][]byte{"token": []byte("plaintext")}))

	badKey := make([]byte, 32)
	badKey[0] = 1
	_, err := New(persistence, secret.NewStaticKeyProvider(badKey)).Get(t.Context(), "default", "creds")
	require.Error(t, err)
	require.False(t, errors.Is(err, secret.ErrPayloadNotFound))
	require.NotContains(t, err.Error(), "plaintext")
}
