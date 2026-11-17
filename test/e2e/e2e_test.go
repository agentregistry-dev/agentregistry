//go:build e2e

package e2e

import (
	"log"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	log.SetPrefix("[e2e] ")
	log.SetFlags(log.Ltime)

	var cleanup func()
	registryURL, cleanup = StartRegistry("E2E_LOCAL_PORT")

	log.Printf("Configuration:")
	log.Printf("  ARCTL_API_BASE_URL: %s", registryURL)
	log.Printf("  GOOGLE_API_KEY:     %s", maskEnv("GOOGLE_API_KEY"))
	log.Printf("  Cluster:            %s (context: %s)", ClusterName, KubeContext)

	code := m.Run()
	cleanup()
	os.Exit(code)
}

func maskEnv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		return "(not set)"
	}
	if len(val) <= 8 {
		return "****"
	}
	return val[:4] + "****"
}

// TestArctlVersion verifies the "arctl version" command succeeds and
// returns version information for both the CLI and the server.
func TestArctlVersion(t *testing.T) {
	tmpDir := t.TempDir()
	result := RunArctl(t, tmpDir, "version")
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "arctl version")
	RequireOutputContains(t, result, "Server version:")
}

// TestRegistryHealth verifies the registry health endpoint responds with 200.
func TestRegistryHealth(t *testing.T) {
	regURL := RegistryURL(t)
	WaitForHealth(t, regURL+"/ping", 30*time.Second)

	resp := RegistryGet(t, regURL+"/version")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 from version endpoint, got %d", resp.StatusCode)
	}
}
