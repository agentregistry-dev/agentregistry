//go:build e2e

package kagent

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	e2e "github.com/agentregistry-dev/agentregistry/test/e2e"
)

func TestMain(m *testing.M) {
	log.SetPrefix("[e2e/kagent] ")
	log.SetFlags(log.Ltime)

	var cleanup func()
	registryURL, cleanup = e2e.StartRegistry("E2E_KAGENT_LOCAL_PORT")
	log.Printf("ARCTL_API_BASE_URL: %s (cluster %s, context %s)", registryURL, e2e.ClusterName, e2e.KubeContext)

	code := m.Run()
	cleanup()
	os.Exit(code)
}

var registryURL string

// registryBaseURL returns the agentregistry URL set during TestMain setup.
func registryBaseURL(t *testing.T) string {
	t.Helper()
	if registryURL == "" {
		t.Fatal("registryURL not set -- infrastructure setup may have failed")
	}
	return registryURL
}

// testDir returns the directory containing these test source files.
// Uses runtime.Caller so it works regardless of working directory.
func testDir() string {
	_, f, _, ok := runtime.Caller(0)
	if !ok {
		panic("runtime.Caller failed")
	}
	return filepath.Dir(f)
}
