package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestStoreRefusesUnmanagedObject(t *testing.T) {
	const backingNamespace = "agentregistry"
	objectName := secret.ObjectName("tenant", "creds")
	unmanaged := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: objectName, Namespace: backingNamespace},
		Data:       map[string][]byte{"token": []byte("do-not-touch")},
	}
	client := fake.NewClientset(unmanaged)
	store := New(client.CoreV1(), backingNamespace)

	require.ErrorIs(t, store.Put(t.Context(), "tenant", "creds", map[string][]byte{"token": []byte("replacement")}), ErrUnmanagedSecret)
	_, err := store.Get(t.Context(), "tenant", "creds")
	require.ErrorIs(t, err, ErrUnmanagedSecret)
	require.ErrorIs(t, store.Delete(t.Context(), "tenant", "creds"), ErrUnmanagedSecret)

	got, err := client.CoreV1().Secrets(backingNamespace).Get(t.Context(), objectName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, []byte("do-not-touch"), got.Data["token"])
}
