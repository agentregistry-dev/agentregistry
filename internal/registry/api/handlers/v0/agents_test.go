package v0_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	v0 "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0"
	servicetesting "github.com/agentregistry-dev/agentregistry/internal/registry/service/testing"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateAgentAcceptsExtraTopLevelElements(t *testing.T) {
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	fake := servicetesting.NewFakeRegistry()

	fake.CreateAgentFn = func(_ context.Context, req *models.AgentJSON) (*models.AgentResponse, error) {
		require.Equal(t, "emailTemplateAgent", req.Name)
		require.Equal(t, "1.0.0", req.Version)
		require.Contains(t, req.AdditionalElements, "card")
		require.Contains(t, req.AdditionalElements, "deployment")

		return &models.AgentResponse{
			Agent: models.AgentJSON{
				AgentManifest: models.AgentManifest{
					Name:          req.Name,
					Framework:     req.Framework,
					Language:      req.Language,
					Description:   req.Description,
					Image:         req.Image,
					ModelName:     req.ModelName,
					ModelProvider: req.ModelProvider,
				},
				Version: req.Version,
				Status:  "active",
			},
			ExtraElements: req.AdditionalElements,
		}, nil
	}

	v0.RegisterAgentsCreateEndpoint(api, "/v0", fake)

	body := map[string]any{
		"name":          "emailTemplateAgent",
		"version":       "1.0.0",
		"framework":     "custom",
		"language":      "python",
		"description":   "Template stateless email-channel agent",
		"image":         "",
		"modelProvider": "openai",
		"modelName":     "gpt-4o-mini",
		"card": map[string]any{
			"name":    "emailTemplateAgent",
			"version": "1.0.0",
		},
		"deployment": map[string]any{
			"namespace": "email-template-dev",
		},
	}

	payload, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v0/agents", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"extraElements"`)
	assert.Contains(t, w.Body.String(), `"card"`)
	assert.Contains(t, w.Body.String(), `"deployment"`)
}
