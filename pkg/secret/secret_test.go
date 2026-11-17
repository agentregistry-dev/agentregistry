package secret

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

type memoryStore struct{ data map[string]map[string][]byte }

func (s *memoryStore) Put(_ context.Context, namespace, name string, data map[string][]byte) error {
	s.data[namespace+"/"+name] = data
	return nil
}

func (s *memoryStore) Get(_ context.Context, namespace, name string) (map[string][]byte, error) {
	data, ok := s.data[namespace+"/"+name]
	if !ok {
		return nil, ErrPayloadNotFound
	}
	return data, nil
}

func (s *memoryStore) Delete(_ context.Context, namespace, name string) error {
	delete(s.data, namespace+"/"+name)
	return nil
}

func TestServiceReturnsRedactedValues(t *testing.T) {
	store := &memoryStore{data: map[string]map[string][]byte{}}
	service := NewService(store)
	require.NoError(t, service.PutPayload(context.Background(), "", "creds", map[string][]byte{"token": []byte("plaintext")}))

	value, err := service.Resolve(context.Background(), v1alpha1.SecretRef{Name: "creds", Key: "token"})
	require.NoError(t, err)
	require.Equal(t, []byte("plaintext"), value.Reveal())
	require.Equal(t, "[REDACTED]", value.String())
	require.Equal(t, "[REDACTED]", fmt.Sprintf("%v", value))
	require.Equal(t, "[REDACTED]", fmt.Sprintf("%#v", value))
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	require.Equal(t, `"[REDACTED]"`, string(encoded))
	require.Equal(t, "[REDACTED]", value.LogValue().Resolve().String())
}

func TestSensitiveValueRedaction(t *testing.T) {
	value := NewSensitiveValue([]byte("super-secret-token"))

	require.Equal(t, "[REDACTED]", value.String())
	require.Equal(t, "[REDACTED]", fmt.Sprintf("%v", value))
	require.Equal(t, "[REDACTED]", fmt.Sprintf("%s", value))
	require.Equal(t, "[REDACTED]", fmt.Sprintf("%#v", value))
	require.Equal(t, "[REDACTED]", fmt.Sprintf("%q", value))
	require.Equal(t, "[REDACTED]", fmt.Sprintf("%x", value))

	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	require.Equal(t, `"[REDACTED]"`, string(encoded))

	encoded, err = json.Marshal(map[string]SensitiveValue{"key": value})
	require.NoError(t, err)
	require.JSONEq(t, `{"key":"[REDACTED]"}`, string(encoded))

	text, err := value.MarshalText()
	require.NoError(t, err)
	require.Equal(t, "[REDACTED]", string(text))

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	logger.Info("value", "secret", value)
	require.Contains(t, logs.String(), "[REDACTED]")
	require.NotContains(t, logs.String(), "super-secret-token")
}

func TestSensitiveValueCopiesPlaintext(t *testing.T) {
	original := []byte("value")
	value := NewSensitiveValue(original)
	original[0] = 'X'
	require.Equal(t, []byte("value"), value.Reveal())

	revealed := value.Reveal()
	revealed[0] = 'X'
	require.Equal(t, []byte("value"), value.Reveal())
	require.Equal(t, len("value"), value.Len())
}
