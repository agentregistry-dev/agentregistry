package daemon

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

type HealthChecker struct {
	BaseURL string
}

// IsResponding checks if the server is responding
func (h *HealthChecker) IsResponding() bool {
	httpClient := &http.Client{Timeout: 2 * time.Second}
	pingURL := strings.TrimRight(h.BaseURL, "/") + "/ping"

	const maxRetries = 3
	for i := range maxRetries {
		resp, err := httpClient.Get(pingURL)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		if i < maxRetries-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}
	return false
}

func (h *HealthChecker) WaitForReady(timeout time.Duration) error {
	pingURL := strings.TrimRight(h.BaseURL, "/") + "/ping"
	httpClient := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	delay := 500 * time.Millisecond

	for time.Now().Before(deadline) {
		resp, err := httpClient.Get(pingURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(delay)
		delay = min(delay*2, 4*time.Second)
	}
	return fmt.Errorf("daemon did not become ready within %s", timeout)
}
