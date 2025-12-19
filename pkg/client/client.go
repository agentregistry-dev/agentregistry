package client

import (
	"github.com/agentregistry-dev/agentregistry/internal/client"
)

// Exposing internal client for external use
func NewClientFromEnv() (*client.Client, error) {
	return client.NewClientFromEnv()
}

func NewClient(baseURL, token string) *client.Client {
	return client.NewClient(baseURL, token)
}
