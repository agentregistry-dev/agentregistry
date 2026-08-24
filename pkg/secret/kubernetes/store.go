// Package kubernetes stores Secret payloads in Kubernetes Secret objects.
package kubernetes

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/agentregistry-dev/agentregistry/pkg/secret"
)

const (
	managedByLabel = "agentregistry.solo.io/managed-by"
	managedByValue = "secret-store"
)

type store struct {
	secrets   v1.SecretInterface
	namespace string
}

// New creates a Kubernetes payload store in namespace.
func New(client v1.CoreV1Interface, namespace string) secret.Store {
	return &store{secrets: client.Secrets(namespace), namespace: namespace}
}

func (*store) Type() secret.StoreType { return secret.StoreTypeKubernetes }

func (s *store) Put(ctx context.Context, namespace, name string, data map[string][]byte) error {
	objectName := secret.ObjectName(namespace, name)
	desired := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name: objectName, Namespace: s.namespace,
		Labels: map[string]string{managedByLabel: managedByValue},
	}, Type: corev1.SecretTypeOpaque, Data: data}
	current, err := s.secrets.Get(ctx, objectName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = s.secrets.Create(ctx, desired, metav1.CreateOptions{})
	} else if err == nil {
		desired.ResourceVersion = current.ResourceVersion
		_, err = s.secrets.Update(ctx, desired, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("write secret object %s/%s: %w", s.namespace, objectName, err)
	}
	return nil
}

func (s *store) Get(ctx context.Context, namespace, name string) (map[string][]byte, error) {
	obj, err := s.secrets.Get(ctx, secret.ObjectName(namespace, name), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, secret.ErrPayloadNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get secret object: %w", err)
	}
	out := make(map[string][]byte, len(obj.Data))
	for key, value := range obj.Data {
		out[key] = append([]byte(nil), value...)
	}
	return out, nil
}

func (s *store) Delete(ctx context.Context, namespace, name string) error {
	err := s.secrets.Delete(ctx, secret.ObjectName(namespace, name), metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete secret object: %w", err)
	}
	return nil
}
