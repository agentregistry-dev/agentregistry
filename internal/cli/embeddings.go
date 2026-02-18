package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	v0 "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0"
	"github.com/agentregistry-dev/agentregistry/internal/registry/jobs"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/spf13/cobra"
)

var (
	embeddingsBatchSize      int
	embeddingsForceUpdate    bool
	embeddingsDryRun         bool
	embeddingsIncludeServers bool
	embeddingsIncludeAgents  bool
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
	embeddingsGenerateCmd.Flags().BoolVar(&embeddingsStream, "stream", true, "Use SSE streaming for progress updates")
	embeddingsGenerateCmd.Flags().DurationVar(&embeddingsPollInterval, "poll-interval", 2*time.Second, "Poll interval when not using streaming")
	EmbeddingsCmd.AddCommand(embeddingsGenerateCmd)
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

func runEmbeddingsGenerate(ctx context.Context) error {
	if !embeddingsIncludeServers && !embeddingsIncludeAgents {
		return fmt.Errorf("no targets selected; use --servers or --agents")
	}

	c, err := client.NewClientFromEnv()
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	defer c.Close()

	req := v0.IndexRequest{
		BatchSize:      embeddingsBatchSize,
		Force:          embeddingsForceUpdate,
		DryRun:         embeddingsDryRun,
		IncludeServers: embeddingsIncludeServers,
		IncludeAgents:  embeddingsIncludeAgents,
	}

	if embeddingsStream {
		return streamIndex(ctx, c, req)
	}
	return pollIndex(ctx, c, req)
}

func streamIndex(ctx context.Context, c *client.Client, req v0.IndexRequest) error {
	httpReq, err := c.NewSSERequest(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	sseClient := c.SSEClient()
	resp, err := sseClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to connect to API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		return fmt.Errorf("indexing job already running")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
	}

	fmt.Println("Starting embeddings indexing (streaming)...")

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if len(line) > 5 && line[:5] == "data:" { //nolint:nestif
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
				var stats service.IndexStats
				if err := json.Unmarshal(event.Stats, &stats); err == nil {
					fmt.Printf("[%s] progress: processed=%d updated=%d skipped=%d failures=%d\n",
						event.Resource, stats.Processed, stats.Updated, stats.Skipped, stats.Failures)
				}
			case "completed":
				fmt.Println("Embedding indexing complete.")
				var result jobs.JobResult
				if err := json.Unmarshal(event.Result, &result); err == nil {
					fmt.Printf("  Servers: processed=%d updated=%d skipped=%d failures=%d\n",
						result.ServersProcessed, result.ServersUpdated, result.ServersSkipped, result.ServerFailures)
					fmt.Printf("  Agents: processed=%d updated=%d skipped=%d failures=%d\n",
						result.AgentsProcessed, result.AgentsUpdated, result.AgentsSkipped, result.AgentFailures)

					totalFailures := result.ServerFailures + result.AgentFailures
					if totalFailures > 0 {
						return fmt.Errorf("%d embedding(s) failed; see logs for details", totalFailures)
					}
				}
				return nil
			case "error":
				return fmt.Errorf("indexing failed: %s", event.Error)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("connection error: %w", err)
	}

	return nil
}

func pollIndex(ctx context.Context, c *client.Client, req v0.IndexRequest) error {
	jobResp, err := c.StartIndex(req)
	if err != nil {
		return fmt.Errorf("failed to start indexing: %w", err)
	}

	fmt.Printf("Started indexing job: %s\n", jobResp.JobID)

	ticker := time.NewTicker(embeddingsPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			status, err := c.GetIndexStatus(jobResp.JobID)
			if err != nil {
				fmt.Printf("Warning: failed to get job status: %v\n", err)
				continue
			}

			fmt.Printf("Progress: processed=%d updated=%d skipped=%d failures=%d\n",
				status.Progress.Processed, status.Progress.Updated, status.Progress.Skipped, status.Progress.Failures)

			if status.Status == "completed" {
				fmt.Println("Embedding indexing complete.")
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
				return fmt.Errorf("indexing failed: %s", errMsg)
			}
		}
	}
}
