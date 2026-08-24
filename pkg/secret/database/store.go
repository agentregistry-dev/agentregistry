// Package database stores encrypted Secret payloads in a persistence backend.
package database

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/secret"
)

// Persistence owns ciphertext rows, allowing callers to retain existing tables.
type Persistence interface {
	UpsertSecretPayload(ctx context.Context, namespace, name string, ciphertext []byte) error
	GetSecretPayload(ctx context.Context, namespace, name string) ([]byte, error)
	DeleteSecretPayload(ctx context.Context, namespace, name string) error
}

type store struct {
	persistence Persistence
	keys        secret.KeyProvider
}

// New creates an encrypted payload store over the supplied persistence backend.
func New(persistence Persistence, keys secret.KeyProvider) secret.Store {
	return &store{persistence: persistence, keys: keys}
}

func (*store) Type() secret.StoreType { return secret.StoreTypeDatabase }

func (s *store) Put(ctx context.Context, namespace, name string, data map[string][]byte) error {
	key, err := s.keys.Key(ctx)
	if err != nil {
		return err
	}
	plaintext, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode secret payload: %w", err)
	}
	ciphertext, err := encrypt(plaintext, key)
	if err != nil {
		return fmt.Errorf("encrypt secret payload: %w", err)
	}
	if err := s.persistence.UpsertSecretPayload(ctx, namespace, name, ciphertext); err != nil {
		return fmt.Errorf("persist secret payload: %w", err)
	}
	return nil
}

func (s *store) Get(ctx context.Context, namespace, name string) (map[string][]byte, error) {
	ciphertext, err := s.persistence.GetSecretPayload(ctx, namespace, name)
	if err != nil {
		if errors.Is(err, pkgdb.ErrNotFound) {
			return nil, secret.ErrPayloadNotFound
		}
		return nil, err
	}
	key, err := s.keys.Key(ctx)
	if err != nil {
		return nil, err
	}
	plaintext, err := decrypt(ciphertext, key)
	if err != nil {
		return nil, errors.New("decrypt secret payload: invalid key or corrupt ciphertext")
	}
	var data map[string][]byte
	if err := json.Unmarshal(plaintext, &data); err != nil {
		return nil, fmt.Errorf("decode secret payload: %w", err)
	}
	return data, nil
}

func (s *store) Delete(ctx context.Context, namespace, name string) error {
	if err := s.persistence.DeleteSecretPayload(ctx, namespace, name); err != nil {
		return fmt.Errorf("delete secret payload: %w", err)
	}
	return nil
}

func encrypt(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func decrypt(ciphertext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	return gcm.Open(nil, ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():], nil)
}
