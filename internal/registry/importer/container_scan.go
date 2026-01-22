package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

type containerImageSummary struct {
	Registry           string
	Image              string
	PullCount          int64
	StarCount          int64
	LastUpdatedAt      *time.Time
	LatestTag          string
	LatestTagUpdatedAt *time.Time
}

func (c *containerImageSummary) summaryString() string {
	if c == nil {
		return ""
	}
	parts := []string{fmt.Sprintf("containers: %s", c.Image)}
	if c.PullCount > 0 {
		parts = append(parts, fmt.Sprintf("pulls=%d", c.PullCount))
	}
	if c.LastUpdatedAt != nil {
		parts = append(parts, fmt.Sprintf("last %s", c.LastUpdatedAt.Format("2006-01-02")))
	}
	return strings.Join(parts, " ")
}

func (c *containerImageSummary) detailString() string {
	if c == nil {
		return ""
	}
	parts := []string{fmt.Sprintf("image=%s", c.Image)}
	if c.Registry != "" {
		parts = append(parts, fmt.Sprintf("registry=%s", c.Registry))
	}
	if c.PullCount > 0 {
		parts = append(parts, fmt.Sprintf("pulls=%d", c.PullCount))
	}
	if c.StarCount > 0 {
		parts = append(parts, fmt.Sprintf("stars=%d", c.StarCount))
	}
	if c.LastUpdatedAt != nil {
		parts = append(parts, fmt.Sprintf("last=%s", c.LastUpdatedAt.Format(time.RFC3339)))
	}
	if c.LatestTag != "" {
		parts = append(parts, fmt.Sprintf("latest_tag=%s", c.LatestTag))
		if c.LatestTagUpdatedAt != nil {
			parts = append(parts, fmt.Sprintf("tag_updated=%s", c.LatestTagUpdatedAt.Format(time.RFC3339)))
		}
	}
	return "containers: " + strings.Join(parts, "; ")
}

func fetchDockerHubSummary(ctx context.Context, client *http.Client, owner, repo string, server *apiv0.ServerJSON) (*containerImageSummary, error) {
	if client == nil {
		client = http.DefaultClient
	}
	ownerSlug := strings.ToLower(owner)
	repoSlug := strings.ToLower(repo)
	for _, pkg := range server.Packages {
		if pkg.RegistryType == "oci" {
			// parse owner/ repo from identifier
			// eg "identifier": "docker.io/ivanmurzakdev/unity-mcp-server:0.17.0",
			parts := strings.SplitN(pkg.Identifier, "/", 3)
			if len(parts) >= 3 {
				dockerOwner := parts[1]
				dockerRepo := parts[2]
				// remove tag if any
				if idx := strings.Index(dockerRepo, ":"); idx >= 0 {
					dockerRepo = dockerRepo[:idx]
				}
				ownerSlug = dockerOwner
				repoSlug = dockerRepo
				break
			}
		}
	}
	base := fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/%s", url.PathEscape(ownerSlug), url.PathEscape(repoSlug))
	repoInfo, err := dockerHubGetRepo(ctx, client, base)
	if err != nil || repoInfo == nil {
		return repoInfo, err
	}
	if tags, err := dockerHubGetLatestTag(ctx, client, base); err == nil && tags != nil {
		repoInfo.LatestTag = tags.Name
		repoInfo.LatestTagUpdatedAt = tags.LastUpdatedAt
	}
	return repoInfo, nil
}

func dockerHubGetRepo(ctx context.Context, client *http.Client, endpoint string) (*containerImageSummary, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("docker hub status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Namespace   string    `json:"namespace"`
		Name        string    `json:"name"`
		PullCount   int64     `json:"pull_count"`
		StarCount   int64     `json:"star_count"`
		LastUpdated time.Time `json:"last_updated"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	_ = resp.Body.Close()
	info := &containerImageSummary{
		Registry:      "dockerhub",
		Image:         fmt.Sprintf("%s/%s", payload.Namespace, payload.Name),
		PullCount:     payload.PullCount,
		StarCount:     payload.StarCount,
		LastUpdatedAt: nil,
	}
	if !payload.LastUpdated.IsZero() {
		last := payload.LastUpdated.UTC()
		info.LastUpdatedAt = &last
	}
	return info, nil
}

type dockerTagInfo struct {
	Name          string
	LastUpdatedAt *time.Time
}

func dockerHubGetLatestTag(ctx context.Context, client *http.Client, base string) (*dockerTagInfo, error) {
	endpoint := base + "/tags?page_size=1&ordering=last_updated"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("docker hub tags status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Results []struct {
			Name        string    `json:"name"`
			LastUpdated time.Time `json:"last_updated"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	_ = resp.Body.Close()
	if len(payload.Results) == 0 {
		return nil, nil
	}
	res := payload.Results[0]
	info := &dockerTagInfo{Name: res.Name}
	if !res.LastUpdated.IsZero() {
		last := res.LastUpdated.UTC()
		info.LastUpdatedAt = &last
	}
	return info, nil
}
