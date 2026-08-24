package v1alpha1

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecretMergedDataAndStripValues(t *testing.T) {
	secret := &Secret{Spec: SecretSpec{
		Data:       map[string]string{"token": base64.StdEncoding.EncodeToString([]byte("encoded"))},
		StringData: map[string]string{"token": "raw", "user": "alice"},
	}}
	data, err := secret.MergedData()
	require.NoError(t, err)
	require.Equal(t, []byte("raw"), data["token"])

	secret.StripValues(data)
	require.Nil(t, secret.Spec.Data)
	require.Nil(t, secret.Spec.StringData)
	require.Equal(t, []string{"token", "user"}, secret.Status.DataKeys)
}
