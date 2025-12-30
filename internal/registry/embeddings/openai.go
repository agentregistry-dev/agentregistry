package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
)

type openAIProvider struct {
	cfg        *config.EmbeddingsConfig
	httpClient *http.Client
}

type openAIEmbeddingRequest struct {
	Input string `json:"input"`
	Model string `json:"model"`
}

type openAIEmbeddingResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

func newOpenAIProvider(cfg *config.EmbeddingsConfig, httpClient *http.Client) Provider {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 30 * time.Second,
		}
	}
	return &openAIProvider{
		cfg:        cfg,
		httpClient: httpClient,
	}
}

func (p *openAIProvider) Generate(ctx context.Context, payload Payload) (*Result, error) {
	if payload.Text == "" {
		return nil, fmt.Errorf("embedding payload text cannot be empty")
	}
	if p.cfg.OpenAIAPIKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required when embeddings are enabled")
	}

	reqBody, err := json.Marshal(openAIEmbeddingRequest{
		Input: payload.Text,
		Model: p.cfg.Model,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal embedding request: %w", err)
	}

	endpoint := strings.TrimRight(p.cfg.OpenAIBaseURL, "/") + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.cfg.OpenAIAPIKey)
	if p.cfg.OpenAIOrg != "" {
		req.Header.Set("OpenAI-Organization", p.cfg.OpenAIOrg)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read embedding response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("embedding provider returned %d: %s", resp.StatusCode, string(body))
	}

	var parsed openAIEmbeddingResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode embedding response: %w", err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("embedding provider error: %s (%s)", parsed.Error.Message, parsed.Error.Code)
	}
	if len(parsed.Data) == 0 {
		return nil, fmt.Errorf("embedding provider returned no data")
	}

	vector := make([]float32, len(parsed.Data[0].Embedding))
	for i, value := range parsed.Data[0].Embedding {
		vector[i] = float32(value)
	}

	return &Result{
		Vector:      vector,
		Provider:    "openai",
		Model:       p.cfg.Model,
		Dimensions:  len(vector),
		GeneratedAt: time.Now().UTC(),
	}, nil
}
