package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var (
	embeddingsBatchSize      int
	embeddingsForceUpdate    bool
	embeddingsDryRun         bool
	embeddingsIncludeServers bool
	embeddingsIncludeAgents  bool
	embeddingsAPIURL         string
	embeddingsStream         bool
	embeddingsPollInterval   time.Duration
)

// EmbeddingsCmd hosts semantic embedding maintenance subcommands.
var EmbeddingsCmd = &cobra.Command{
	Use:   "embeddings",
	Short: "Manage semantic embeddings stored in the registry database",
}

var embeddingsGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate embeddings for existing servers and agents (backfill or refresh)",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		return runEmbeddingsGenerate(ctx)
	},
}

func init() {
	embeddingsGenerateCmd.Flags().IntVar(&embeddingsBatchSize, "batch-size", 100, "Number of server versions processed per batch")
	embeddingsGenerateCmd.Flags().BoolVar(&embeddingsForceUpdate, "update", false, "Regenerate embeddings even when the stored checksum matches")
	embeddingsGenerateCmd.Flags().BoolVar(&embeddingsDryRun, "dry-run", false, "Print planned changes without calling the embedding provider or writing to the database")
	embeddingsGenerateCmd.Flags().BoolVar(&embeddingsIncludeServers, "servers", true, "Include MCP servers when generating embeddings")
	embeddingsGenerateCmd.Flags().BoolVar(&embeddingsIncludeAgents, "agents", true, "Include agents when generating embeddings")
	embeddingsGenerateCmd.Flags().StringVar(&embeddingsAPIURL, "api-url", "", "Registry API URL (or set AGENT_REGISTRY_API_URL)")
	embeddingsGenerateCmd.Flags().BoolVar(&embeddingsStream, "stream", true, "Use SSE streaming for progress updates")
	embeddingsGenerateCmd.Flags().DurationVar(&embeddingsPollInterval, "poll-interval", 2*time.Second, "Poll interval when not using streaming")
	EmbeddingsCmd.AddCommand(embeddingsGenerateCmd)
}

// backfillRequest is the request body for starting a backfill job.
type backfillRequest struct {
	BatchSize      int  `json:"batchSize,omitempty"`
	Force          bool `json:"force,omitempty"`
	DryRun         bool `json:"dryRun,omitempty"`
	IncludeServers bool `json:"includeServers,omitempty"`
	IncludeAgents  bool `json:"includeAgents,omitempty"`
	Stream         bool `json:"stream,omitempty"`
}

// backfillJobResponse is the response for job creation.
type backfillJobResponse struct {
	JobID  string `json:"jobId"`
	Status string `json:"status"`
}

// jobStatusResponse is the response for job status.
type jobStatusResponse struct {
	JobID    string `json:"jobId"`
	Type     string `json:"type"`
	Status   string `json:"status"`
	Progress struct {
		Total     int `json:"total"`
		Processed int `json:"processed"`
		Updated   int `json:"updated"`
		Skipped   int `json:"skipped"`
		Failures  int `json:"failures"`
	} `json:"progress"`
	Result *struct {
		ServersProcessed int    `json:"serversProcessed,omitempty"`
		ServersUpdated   int    `json:"serversUpdated,omitempty"`
		ServersSkipped   int    `json:"serversSkipped,omitempty"`
		ServerFailures   int    `json:"serverFailures,omitempty"`
		AgentsProcessed  int    `json:"agentsProcessed,omitempty"`
		AgentsUpdated    int    `json:"agentsUpdated,omitempty"`
		AgentsSkipped    int    `json:"agentsSkipped,omitempty"`
		AgentFailures    int    `json:"agentFailures,omitempty"`
		Error            string `json:"error,omitempty"`
	} `json:"result,omitempty"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// sseEvent represents a server-sent event.
type sseEvent struct {
	Type     string          `json:"type"`
	JobID    string          `json:"jobId,omitempty"`
	Resource string          `json:"resource,omitempty"`
	Stats    json.RawMessage `json:"stats,omitempty"`
	Result   json.RawMessage `json:"result,omitempty"`
	Error    string          `json:"error,omitempty"`
}

func getAPIURL() string {
	if embeddingsAPIURL != "" {
		return embeddingsAPIURL
	}
	return os.Getenv("AGENT_REGISTRY_API_URL")
}

func runEmbeddingsGenerate(ctx context.Context) error {
	apiURL := getAPIURL()
	if apiURL == "" {
		return fmt.Errorf("--api-url or AGENT_REGISTRY_API_URL required")
	}

	if !embeddingsIncludeServers && !embeddingsIncludeAgents {
		return fmt.Errorf("no targets selected; use --servers or --agents")
	}

	req := backfillRequest{
		BatchSize:      embeddingsBatchSize,
		Force:          embeddingsForceUpdate,
		DryRun:         embeddingsDryRun,
		IncludeServers: embeddingsIncludeServers,
		IncludeAgents:  embeddingsIncludeAgents,
		Stream:         false, // Always false for POST, we use GET for streaming
	}

	if embeddingsStream {
		return streamBackfill(ctx, apiURL, req)
	}
	return pollBackfill(ctx, apiURL, req)
}

func streamBackfill(ctx context.Context, apiURL string, req backfillRequest) error {
	// Build SSE streaming URL with query parameters
	streamURL := fmt.Sprintf("%s/admin/v0/embeddings/backfill/stream?batchSize=%d&force=%t&dryRun=%t&includeServers=%t&includeAgents=%t",
		apiURL, req.BatchSize, req.Force, req.DryRun, req.IncludeServers, req.IncludeAgents)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: 0} // No timeout for SSE
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to connect to API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		return fmt.Errorf("backfill job already running")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
	}

	fmt.Println("Starting embeddings backfill (streaming)...")

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if len(line) > 5 && line[:5] == "data:" {
			data := line[5:]
			if len(data) > 0 && data[0] == ' ' {
				data = data[1:]
			}

			var event sseEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}

			switch event.Type {
			case "started":
				fmt.Printf("Job started: %s\n", event.JobID)
			case "progress":
				var stats struct {
					Processed int `json:"processed"`
					Updated   int `json:"updated"`
					Skipped   int `json:"skipped"`
					Failures  int `json:"failures"`
				}
				if err := json.Unmarshal(event.Stats, &stats); err == nil {
					fmt.Printf("[%s] progress: processed=%d updated=%d skipped=%d failures=%d\n",
						event.Resource, stats.Processed, stats.Updated, stats.Skipped, stats.Failures)
				}
			case "completed":
				fmt.Println("Embedding backfill complete.")
				var result struct {
					Servers struct {
						Processed int `json:"processed"`
						Updated   int `json:"updated"`
						Skipped   int `json:"skipped"`
						Failures  int `json:"failures"`
					} `json:"servers"`
					Agents struct {
						Processed int `json:"processed"`
						Updated   int `json:"updated"`
						Skipped   int `json:"skipped"`
						Failures  int `json:"failures"`
					} `json:"agents"`
				}
				if err := json.Unmarshal(event.Result, &result); err == nil {
					fmt.Printf("  Servers: processed=%d updated=%d skipped=%d failures=%d\n",
						result.Servers.Processed, result.Servers.Updated, result.Servers.Skipped, result.Servers.Failures)
					fmt.Printf("  Agents: processed=%d updated=%d skipped=%d failures=%d\n",
						result.Agents.Processed, result.Agents.Updated, result.Agents.Skipped, result.Agents.Failures)

					totalFailures := result.Servers.Failures + result.Agents.Failures
					if totalFailures > 0 {
						return fmt.Errorf("%d embedding(s) failed; see logs for details", totalFailures)
					}
				}
				return nil
			case "error":
				return fmt.Errorf("backfill failed: %s", event.Error)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("connection error: %w", err)
	}

	return nil
}

func pollBackfill(ctx context.Context, apiURL string, req backfillRequest) error {
	// Start the backfill job
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	backfillURL := fmt.Sprintf("%s/admin/v0/embeddings/backfill", apiURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, backfillURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to connect to API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		return fmt.Errorf("backfill job already running")
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (%d): %s", resp.StatusCode, string(respBody))
	}

	var jobResp backfillJobResponse
	if err := json.NewDecoder(resp.Body).Decode(&jobResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	fmt.Printf("Started backfill job: %s\n", jobResp.JobID)

	// Poll for job status
	statusURL := fmt.Sprintf("%s/admin/v0/embeddings/backfill/%s", apiURL, jobResp.JobID)
	ticker := time.NewTicker(embeddingsPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			status, err := getJobStatus(ctx, client, statusURL)
			if err != nil {
				fmt.Printf("Warning: failed to get job status: %v\n", err)
				continue
			}

			fmt.Printf("Progress: processed=%d updated=%d skipped=%d failures=%d\n",
				status.Progress.Processed, status.Progress.Updated, status.Progress.Skipped, status.Progress.Failures)

			if status.Status == "completed" {
				fmt.Println("Embedding backfill complete.")
				if status.Result != nil {
					fmt.Printf("  Servers: processed=%d updated=%d skipped=%d failures=%d\n",
						status.Result.ServersProcessed, status.Result.ServersUpdated, status.Result.ServersSkipped, status.Result.ServerFailures)
					fmt.Printf("  Agents: processed=%d updated=%d skipped=%d failures=%d\n",
						status.Result.AgentsProcessed, status.Result.AgentsUpdated, status.Result.AgentsSkipped, status.Result.AgentFailures)

					totalFailures := status.Result.ServerFailures + status.Result.AgentFailures
					if totalFailures > 0 {
						return fmt.Errorf("%d embedding(s) failed; see logs for details", totalFailures)
					}
				}
				return nil
			}

			if status.Status == "failed" {
				errMsg := "unknown error"
				if status.Result != nil && status.Result.Error != "" {
					errMsg = status.Result.Error
				}
				return fmt.Errorf("backfill failed: %s", errMsg)
			}
		}
	}
}

func getJobStatus(ctx context.Context, client *http.Client, url string) (*jobStatusResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
	}

	var status jobStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, err
	}

	return &status, nil
}
