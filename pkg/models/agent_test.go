package models

import (
	"encoding/json"
	"testing"
)

func TestAgentJSONMarshalIncludesAdditionalElements(t *testing.T) {
	agent := AgentJSON{
		AgentManifest: AgentManifest{
			Name:        "test-agent",
			Language:    "python",
			Framework:   "langgraph",
			Description: "Agent with card metadata",
			AdditionalElements: map[string]any{
				"card": map[string]any{
					"name":    "test-agent",
					"version": "1.2.3",
				},
				"customElement": map[string]any{
					"enabled": true,
				},
			},
		},
		Version: "1.2.3",
		Status:  "active",
	}

	data, err := json.Marshal(agent)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if _, ok := payload["card"]; !ok {
		t.Fatalf("expected card in marshaled payload, got %v", payload)
	}
	if _, ok := payload["customElement"]; !ok {
		t.Fatalf("expected customElement in marshaled payload, got %v", payload)
	}
}

func TestAgentJSONUnmarshalCapturesAdditionalElements(t *testing.T) {
	data := []byte(`{
		"name": "test-agent",
		"language": "python",
		"framework": "langgraph",
		"description": "Agent with extras",
		"version": "1.2.3",
		"card": {
			"name": "test-agent",
			"version": "1.2.3"
		},
		"otherElement": {
			"flag": true
		}
	}`)

	var agent AgentJSON
	if err := json.Unmarshal(data, &agent); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if agent.Name != "test-agent" {
		t.Fatalf("expected name to be decoded, got %q", agent.Name)
	}
	if len(agent.AdditionalElements) != 2 {
		t.Fatalf("expected 2 additional elements, got %d", len(agent.AdditionalElements))
	}
	if _, ok := agent.AdditionalElements["card"]; !ok {
		t.Fatalf("expected card to be captured, got %v", agent.AdditionalElements)
	}
	if _, ok := agent.AdditionalElements["otherElement"]; !ok {
		t.Fatalf("expected otherElement to be captured, got %v", agent.AdditionalElements)
	}
}
