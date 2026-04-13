package deployment

import (
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/stretchr/testify/assert"
)

func TestEnvEqual(t *testing.T) {
	cases := []struct {
		name string
		a, b map[string]string
		want bool
	}{
		{"both nil", nil, nil, true},
		{"nil vs empty", nil, map[string]string{}, true},
		{"empty vs empty", map[string]string{}, map[string]string{}, true},
		{"identical", map[string]string{"K": "V"}, map[string]string{"K": "V"}, true},
		{"value differs", map[string]string{"K": "V1"}, map[string]string{"K": "V2"}, false},
		{"key missing", map[string]string{"K": "V"}, map[string]string{}, false},
		{"extra key", map[string]string{"K": "V"}, map[string]string{"K": "V", "X": "Y"}, false},
		{"order independent", map[string]string{"A": "1", "B": "2"}, map[string]string{"B": "2", "A": "1"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, envEqual(tc.a, tc.b))
		})
	}
}

func TestProviderConfigEqual(t *testing.T) {
	cases := []struct {
		name string
		a, b models.JSONObject
		want bool
	}{
		{"both nil", nil, nil, true},
		{"nil vs empty", nil, models.JSONObject{}, true},
		{"identical scalars", models.JSONObject{"k": "v"}, models.JSONObject{"k": "v"}, true},
		{"identical nested", models.JSONObject{"k": map[string]any{"x": float64(1)}}, models.JSONObject{"k": map[string]any{"x": float64(1)}}, true},
		{"differs scalar", models.JSONObject{"k": "v1"}, models.JSONObject{"k": "v2"}, false},
		{"differs nested", models.JSONObject{"k": map[string]any{"x": float64(1)}}, models.JSONObject{"k": map[string]any{"x": float64(2)}}, false},
		{"extra key", models.JSONObject{"a": "1"}, models.JSONObject{"a": "1", "b": "2"}, false},
		{"key order independent", models.JSONObject{"a": "1", "b": "2"}, models.JSONObject{"b": "2", "a": "1"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, providerConfigEqual(tc.a, tc.b))
		})
	}
}
