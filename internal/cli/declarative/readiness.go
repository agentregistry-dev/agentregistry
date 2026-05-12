package declarative

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// waitForHTTPReady polls url with HTTP GET until it returns a 2xx/3xx
// response, the context is cancelled, or timeout elapses. Returns nil
// on first successful response.
//
// httpGet is injected so tests can substitute a fake; pass nil to use
// the package-level defaultHTTPGet.
func waitForHTTPReady(ctx context.Context, url string, timeout, interval time.Duration, httpGet func(ctx context.Context, url string) (int, error)) error {
	if httpGet == nil {
		httpGet = defaultHTTPGet
	}
	if interval <= 0 {
		interval = 1 * time.Second
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Try once immediately so a fast-starting server returns quickly,
	// then fall through to ticker-based polling.
	if status, err := httpGet(deadlineCtx, url); err == nil && isReady(status) {
		return nil
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-deadlineCtx.Done():
			return fmt.Errorf("timeout after %s waiting for %s", timeout, url)
		case <-ticker.C:
			status, err := httpGet(deadlineCtx, url)
			if err != nil {
				continue
			}
			if isReady(status) {
				return nil
			}
		}
	}
}

// isReady returns true for any HTTP response — the server is alive and
// answering. Even 405 / 404 / 5xx count, because they prove the process
// has bound the port and the TCP handshake completed. Only connection
// failures (handled by httpGet returning an error) mean "not ready yet."
func isReady(status int) bool {
	return status > 0
}

// defaultHTTPGet performs the GET with a short per-request timeout so a
// hung server doesn't block the polling loop.
func defaultHTTPGet(ctx context.Context, url string) (int, error) {
	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	return resp.StatusCode, nil
}
