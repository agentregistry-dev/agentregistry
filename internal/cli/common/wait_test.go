package common

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
)

func TestWaitForDeploymentReady_ReturnsFailureDetails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/deployments/dep-1" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(models.Deployment{
			ID:     "dep-1",
			Status: "failed",
			Error:  "agent demo-agent: DeploymentNotReady",
		})
	}))
	defer srv.Close()

	err := WaitForDeploymentReady(client.NewClient(srv.URL, ""), "dep-1")
	if err == nil {
		t.Fatal("expected error")
	}
	if got, want := err.Error(), "deployment failed: agent demo-agent: DeploymentNotReady"; got != want {
		t.Fatalf("WaitForDeploymentReady() error = %q, want %q", got, want)
	}
}

func TestWaitForDeploymentReady_PollsUntilDeployed(t *testing.T) {
	originalTimeout := defaultWaitTimeout
	originalInterval := defaultPollInterval
	defaultWaitTimeout = 200 * time.Millisecond
	defaultPollInterval = 10 * time.Millisecond
	t.Cleanup(func() {
		defaultWaitTimeout = originalTimeout
		defaultPollInterval = originalInterval
	})

	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/deployments/dep-2" {
			http.NotFound(w, r)
			return
		}
		requests++
		status := "deploying"
		errorText := "agent demo-agent: DeploymentNotReady"
		if requests >= 2 {
			status = "deployed"
			errorText = ""
		}
		_ = json.NewEncoder(w).Encode(models.Deployment{
			ID:     "dep-2",
			Status: status,
			Error:  errorText,
		})
	}))
	defer srv.Close()

	if err := WaitForDeploymentReady(client.NewClient(srv.URL, ""), "dep-2"); err != nil {
		t.Fatalf("WaitForDeploymentReady() error = %v", err)
	}
	if requests < 2 {
		t.Fatalf("expected at least 2 poll requests, got %d", requests)
	}
}
