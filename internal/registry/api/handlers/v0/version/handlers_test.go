package version_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/assert"

	v0version "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/version"
)

func TestVersionEndpoint(t *testing.T) {
	testCases := []struct {
		name         string
		versionInfo  *v0version.VersionBody
		expectedBody map[string]string
	}{
		{
			name: "returns version information",
			versionInfo: &v0version.VersionBody{
				Version:   "v1.2.3",
				GitCommit: "abc123def456",
				BuildTime: "2025-10-14T12:00:00Z",
			},
			expectedBody: map[string]string{
				"version":    "v1.2.3",
				"git_commit": "abc123def456",
				"build_time": "2025-10-14T12:00:00Z",
			},
		},
		{
			name: "returns dev version information",
			versionInfo: &v0version.VersionBody{
				Version:   "dev",
				GitCommit: "unknown",
				BuildTime: "unknown",
			},
			expectedBody: map[string]string{
				"version":    "dev",
				"git_commit": "unknown",
				"build_time": "unknown",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

			v0version.RegisterVersionEndpoint(api, "/v0", tc.versionInfo)

			req := httptest.NewRequest(http.MethodGet, "/v0/version", nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)

			body := w.Body.String()
			assert.Contains(t, body, `"version":"`+tc.expectedBody["version"]+`"`)
			assert.Contains(t, body, `"git_commit":"`+tc.expectedBody["git_commit"]+`"`)
			assert.Contains(t, body, `"build_time":"`+tc.expectedBody["build_time"]+`"`)
		})
	}
}
