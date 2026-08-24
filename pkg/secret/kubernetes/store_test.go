package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/agentregistry-dev/agentregistry/pkg/secret"
)

func TestStoreRoundTrip(t *testing.T) {
	client := fake.NewClientset()
	store := New(client.CoreV1(), "agentregistry")
	payload := map[string][]byte{"token": []byte("plaintext")}

	require.NoError(t, store.Put(t.Context(), "tenant", "creds", payload))
	got, err := store.Get(t.Context(), "tenant", "creds")
	require.NoError(t, err)
	require.Equal(t, payload, got)

	got["token"][0] = 'x'
	again, err := store.Get(t.Context(), "tenant", "creds")
	require.NoError(t, err)
	require.Equal(t, payload, again)

	require.NoError(t, store.Delete(t.Context(), "tenant", "creds"))
	_, err = store.Get(t.Context(), "tenant", "creds")
	require.ErrorIs(t, err, secret.ErrPayloadNotFound)
}
