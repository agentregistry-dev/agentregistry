package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/internal/registry/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"math"
)

// Service handles importing seed data into the registry
type Service struct {
	registry       service.RegistryService
	httpClient     *http.Client
	requestHeaders map[string]string
	updateIfExists bool
	githubToken    string
}

// NewService creates a new importer service with sane defaults
func NewService(registry service.RegistryService) *Service {
	return &Service{
		registry:       registry,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
		requestHeaders: map[string]string{},
	}
}

// (Deprecated) NewServiceWithHTTP was removed; use NewService() and setters instead.

// SetRequestHeaders replaces headers used for HTTP fetches
func (s *Service) SetRequestHeaders(headers map[string]string) {
	s.requestHeaders = headers
}

// SetHTTPClient overrides the HTTP client used for fetches
func (s *Service) SetHTTPClient(client *http.Client) {
	if client != nil {
		s.httpClient = client
	}
}

// SetUpdateIfExists toggles replacing existing name/version entries instead of skipping
func (s *Service) SetUpdateIfExists(update bool) {
	s.updateIfExists = update
}

// SetGitHubToken sets a token used only for GitHub enrichment calls
func (s *Service) SetGitHubToken(token string) {
	s.githubToken = strings.TrimSpace(token)
}

// ImportFromPath imports seed data from various sources:
// 1. Local file paths (*.json files) - expects ServerJSON array format
// 2. Direct HTTP URLs to seed.json files - expects ServerJSON array format
// 3. Registry API endpoints (e.g., /v0/servers, /v0.1/servers) - handles pagination automatically
func (s *Service) ImportFromPath(ctx context.Context, path string) error {
	servers, err := s.readSeedFile(ctx, path)
	if err != nil {
		return fmt.Errorf("failed to read seed data: %w", err)
	}

	// Import each server using registry service CreateServer
	var successfullyCreated []string
	var failedCreations []string
	total := len(servers)
	processed := 0

	for _, server := range servers {
		processed++
		log.Printf("Importing %d/%d: %s@%s", processed, total, server.Name, server.Version)

		// Best-effort enrichment
		if err := s.enrichServer(ctx, server); err != nil {
			log.Printf("Warning: enrichment failed for %s@%s: %v", server.Name, server.Version, err)
		}

		_, err := s.registry.CreateServer(ctx, server)
		if err != nil {
			// If duplicate version and update is enabled, try update path
			if s.updateIfExists && errors.Is(err, database.ErrInvalidVersion) {
				if _, uerr := s.registry.UpdateServer(ctx, server.Name, server.Version, server, nil); uerr != nil {
					failedCreations = append(failedCreations, fmt.Sprintf("%s: %v", server.Name, uerr))
					log.Printf("Failed to update existing server %s: %v", server.Name, uerr)
				} else {
					successfullyCreated = append(successfullyCreated, server.Name)
					continue
				}
			} else {
				failedCreations = append(failedCreations, fmt.Sprintf("%s: %v", server.Name, err))
				log.Printf("Failed to create server %s: %v", server.Name, err)
			}
		} else {
			successfullyCreated = append(successfullyCreated, server.Name)
		}
	}

	// Report import results after actual creation attempts
	if len(failedCreations) > 0 {
		log.Printf("Import completed with errors: %d servers created successfully, %d servers failed",
			len(successfullyCreated), len(failedCreations))
		log.Printf("Failed servers: %v", failedCreations)
		return fmt.Errorf("failed to import %d servers", len(failedCreations))
	}

	log.Printf("Import completed successfully: all %d servers created", len(successfullyCreated))
	return nil
}

// readSeedFile reads seed data from various sources
func (s *Service) readSeedFile(ctx context.Context, path string) ([]*apiv0.ServerJSON, error) {
	var data []byte
	var err error

	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		// Handle HTTP URLs
		if strings.HasSuffix(path, "/servers") {
			// This is a registry API endpoint - fetch paginated data
			return s.fetchFromRegistryAPI(ctx, path)
		}
		// This is a direct file URL
		data, err = s.fetchFromHTTP(ctx, path)
	} else {
		// Handle local file paths
		data, err = os.ReadFile(path)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to read seed data from %s: %w", path, err)
	}

	// Parse ServerJSON array format
	var serverResponses []apiv0.ServerJSON
	if err := json.Unmarshal(data, &serverResponses); err != nil {
		return nil, fmt.Errorf("failed to parse seed data as ServerJSON array format: %w", err)
	}

	if len(serverResponses) == 0 {
		return []*apiv0.ServerJSON{}, nil
	}

	// Validate servers and collect warnings instead of failing the whole batch
	var validRecords []*apiv0.ServerJSON
	var invalidServers []string
	var validationFailures []string

	for _, response := range serverResponses {
		if err := validators.ValidateServerJSON(&response); err != nil {
			// Log warning and track invalid server instead of failing
			invalidServers = append(invalidServers, response.Name)
			validationFailures = append(validationFailures, fmt.Sprintf("Server '%s': %v", response.Name, err))
			log.Printf("Warning: Skipping invalid server '%s': %v", response.Name, err)
			continue
		}

		// Add valid ServerJSON to records
		validRecords = append(validRecords, &response)
	}

	// Print summary of validation results
	if len(invalidServers) > 0 {
		log.Printf("Validation summary: %d servers passed validation, %d invalid servers skipped", len(validRecords), len(invalidServers))
		log.Printf("Invalid servers: %v", invalidServers)
		for _, failure := range validationFailures {
			log.Printf("  - %s", failure)
		}
	} else {
		log.Printf("Validation summary: All %d servers passed validation", len(validRecords))
	}

	return validRecords, nil
}

func (s *Service) fetchFromHTTP(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	// apply custom headers if provided
	for k, v := range s.requestHeaders {
		req.Header.Set(k, v)
	}

	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from HTTP: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP request failed with status: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func (s *Service) fetchFromRegistryAPI(ctx context.Context, baseURL string) ([]*apiv0.ServerJSON, error) {
	var allRecords []*apiv0.ServerJSON
	cursor := ""

	for {
		url := baseURL
		if cursor != "" {
			if strings.Contains(url, "?") {
				url += "&cursor=" + cursor
			} else {
				url += "?cursor=" + cursor
			}
		}

		data, err := s.fetchFromHTTP(ctx, url)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch page from registry API: %w", err)
		}

		var response struct {
			Servers  []apiv0.ServerResponse `json:"servers"`
			Metadata *struct {
				NextCursor string `json:"nextCursor,omitempty"`
			} `json:"metadata,omitempty"`
		}

		if err := json.Unmarshal(data, &response); err != nil {
			return nil, fmt.Errorf("failed to parse registry API response: %w", err)
		}

		// Extract ServerJSON from each ServerResponse
		for _, serverResponse := range response.Servers {
			allRecords = append(allRecords, &serverResponse.Server)
		}

		// Check if there's a next page
		if response.Metadata == nil || response.Metadata.NextCursor == "" {
			break
		}
		cursor = response.Metadata.NextCursor
	}

	return allRecords, nil
}

// enrichServer augments ServerJSON with vendor metadata under _meta.publisher-provided
// Key: agentregistry.solo.io/metadata { stars: <int> }
func (s *Service) enrichServer(ctx context.Context, server *apiv0.ServerJSON) error {
	if server == nil || server.Repository == nil || server.Repository.URL == "" {
		return nil
	}
	owner, repo := parseGitHubRepo(server.Repository.URL)
	if owner == "" || repo == "" {
		return nil
	}

	// Fetch repo summary (stars, forks, watchers, language, topics, timestamps)
	repoSummary, err := s.fetchGitHubRepoSummary(ctx, owner, repo)
	if err != nil {
		return err
	}

	// Fetch releases summary (downloads total, latest published at)
	releasesSummary, err := s.fetchGitHubReleasesSummary(ctx, owner, repo)
	if err != nil {
		return err
	}

	// Compute score per contract: 0.6*log10(stars+1) + 0.4*log10(downloads.total+1)
	score := 0.6*math.Log10(float64(repoSummary.Stars)+1) + 0.4*math.Log10(float64(releasesSummary.TotalDownloads)+1)

	// Determine if version uses semver
	usesSemver := isSemverVersion(server.Version)

	// Fill topics if missing via fallback endpoint
	if len(repoSummary.Topics) == 0 {
		if topics, err := s.fetchGitHubTopics(ctx, owner, repo); err == nil && len(topics) > 0 {
			repoSummary.Topics = topics
		}
	}

	// Fetch tags list (names only) best-effort
	repoTags, _ := s.fetchGitHubTags(ctx, owner, repo, 100)

	// Fetch org verification boolean (best-effort)
	orgIsVerified, _ := s.fetchGitHubOrgIsVerified(ctx, owner)

	// Security scanning heuristics
	dependabotEnabled, _ := s.detectDependabotEnabled(ctx, owner, repo)
	codeqlEnabled, _ := s.detectCodeQLEnabled(ctx, owner, repo)

	// Security alert counts (best-effort, require token)
	var dependabotAlerts interface{} = nil
	var codeScanningAlerts interface{} = nil
	if strings.TrimSpace(s.githubToken) != "" {
		if cnt, err := s.fetchDependabotAlertsCount(ctx, owner, repo); err == nil && cnt != nil {
			dependabotAlerts = *cnt
		}
		if cnt, err := s.fetchCodeScanningAlertsCount(ctx, owner, repo); err == nil && cnt != nil {
			codeScanningAlerts = *cnt
		}
	}

	// OpenSSF Scorecard (public API)
	ossfScore, _ := s.fetchOpenSSFScore(ctx, owner, repo)

	// Endpoint health probe (first remote only)
	var endpointReachable interface{} = nil
	var endpointResponseMs interface{} = nil
	var endpointCheckedAt interface{} = nil
	if len(server.Remotes) > 0 && server.Remotes[0].URL != "" {
		reachable, ms, ts := probeEndpointHealth(ctx, server.Remotes[0].URL)
		endpointReachable = reachable
		if ms != nil {
			endpointResponseMs = *ms
		}
		if ts != nil {
			endpointCheckedAt = ts.UTC().Format(time.RFC3339)
		}
	}

	if server.Meta == nil {
		server.Meta = &apiv0.ServerMeta{}
	}
	if server.Meta.PublisherProvided == nil {
		server.Meta.PublisherProvided = map[string]interface{}{}
	}

	enterprise := map[string]interface{}{
		"stars": repoSummary.Stars,
		"downloads": map[string]interface{}{
			"total":  releasesSummary.TotalDownloads,
			"weekly": nil, // MVP
		},
		"score": score,
		"repo": map[string]interface{}{
			"forks_count":      repoSummary.ForksCount,
			"watchers_count":   repoSummary.WatchersCount,
			"primary_language": repoSummary.PrimaryLanguage,
			"topics":           repoSummary.Topics,
			"tags":             repoTags,
		},
		"activity": map[string]interface{}{
			"created_at": timePtrToRFC3339(repoSummary.CreatedAt),
			"updated_at": timePtrToRFC3339(repoSummary.UpdatedAt),
			"pushed_at":  timePtrToRFC3339(repoSummary.PushedAt),
		},
		"releases": map[string]interface{}{
			"latest_published_at": timePtrToRFC3339(releasesSummary.LatestPublishedAt),
		},
		"identity": map[string]interface{}{
			"publisher_identity_verified_by_jwt": false, // importer lacks JWT context
			"org_is_verified":                    orgIsVerified,
		},
		"semver": map[string]interface{}{
			"uses_semver": usesSemver,
		},
		"scorecard": map[string]interface{}{
			"openssf": ossfScore,
		},
		"endpoint_health": map[string]interface{}{
			"reachable":       endpointReachable,
			"response_ms":     endpointResponseMs,
			"last_checked_at": endpointCheckedAt,
		},
		"security_scanning": map[string]interface{}{
			"codeql_enabled":       codeqlEnabled,
			"dependabot_enabled":   dependabotEnabled,
			"code_scanning_alerts": codeScanningAlerts,
			"dependabot_alerts":    dependabotAlerts,
		},
		"scans": map[string]interface{}{
			"summary": nil,
			"details": []interface{}{},
		},
	}

	server.Meta.PublisherProvided["agentregistry.solo.io/metadata"] = enterprise
	return nil
}

// parseGitHubRepo extracts owner/repo from common GitHub URL formats
func parseGitHubRepo(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, ".git")
	if strings.Contains(raw, "github.com/") {
		parts := strings.Split(raw, "github.com/")
		path := parts[len(parts)-1]
		segs := strings.Split(strings.Trim(path, "/"), "/")
		if len(segs) >= 2 {
			return segs[0], segs[1]
		}
	}
	sshRe := regexp.MustCompile(`github\.com:([^/]+)/([^/]+)$`)
	m := sshRe.FindStringSubmatch(raw)
	if len(m) == 3 {
		return m[1], m[2]
	}
	return "", ""
}

// fetchGitHubStars queries the GitHub repo API for stargazers_count
func (s *Service) fetchGitHubStars(ctx context.Context, owner, repo string) (int, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	// Do NOT forward arbitrary registry headers to GitHub.
	// Only apply an explicit GitHub token if provided.
	if s.githubToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.githubToken)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("github api status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Stars int `json:"stargazers_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, err
	}
	return payload.Stars, nil
}

// fetchGitHubRepoSummary retrieves repository summary fields used for enrichment.
func (s *Service) fetchGitHubRepoSummary(ctx context.Context, owner, repo string) (*githubRepoSummary, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if s.githubToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.githubToken)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github api status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Stars           int       `json:"stargazers_count"`
		ForksCount      int       `json:"forks_count"`
		WatchersCount   int       `json:"watchers_count"`
		PrimaryLanguage *string   `json:"language"`
		Topics          []string  `json:"topics"`
		CreatedAt       time.Time `json:"created_at"`
		UpdatedAt       time.Time `json:"updated_at"`
		PushedAt        time.Time `json:"pushed_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	// Ensure topics is non-nil for JSON marshalling
	if payload.Topics == nil {
		payload.Topics = []string{}
	}
	return &githubRepoSummary{
		Stars:           payload.Stars,
		ForksCount:      payload.ForksCount,
		WatchersCount:   payload.WatchersCount,
		PrimaryLanguage: payload.PrimaryLanguage,
		Topics:          payload.Topics,
		CreatedAt:       &payload.CreatedAt,
		UpdatedAt:       &payload.UpdatedAt,
		PushedAt:        &payload.PushedAt,
	}, nil
}

// fetchGitHubReleasesSummary retrieves releases data to compute downloads total and latest published timestamp.
func (s *Service) fetchGitHubReleasesSummary(ctx context.Context, owner, repo string) (*githubReleasesSummary, error) {
	totalDownloads := 0
	var latest *time.Time
	page := 1
	for {
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases?per_page=100&page=%d", owner, repo, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		if s.githubToken != "" {
			req.Header.Set("Authorization", "Bearer "+s.githubToken)
		}
		if req.Header.Get("Accept") == "" {
			req.Header.Set("Accept", "application/vnd.github+json")
		}
		client := s.httpClient
		if client == nil {
			client = http.DefaultClient
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		var releases []struct {
			PublishedAt *time.Time `json:"published_at"`
			Assets      []struct {
				DownloadCount int `json:"download_count"`
			} `json:"assets"`
		}
		if resp.StatusCode != http.StatusOK {
			// Treat missing releases (404) as zero releases
			if resp.StatusCode == http.StatusNotFound {
				_ = resp.Body.Close()
				break
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return nil, fmt.Errorf("github releases api status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		_ = resp.Body.Close()
		if len(releases) == 0 {
			break
		}
		for _, r := range releases {
			for _, a := range r.Assets {
				totalDownloads += a.DownloadCount
			}
			if r.PublishedAt != nil {
				if latest == nil || r.PublishedAt.After(*latest) {
					latest = r.PublishedAt
				}
			}
		}
		page++
	}
	return &githubReleasesSummary{TotalDownloads: totalDownloads, LatestPublishedAt: latest}, nil
}

// githubRepoSummary captures fields from the GitHub repo API used for enrichment.
type githubRepoSummary struct {
	Stars           int
	ForksCount      int
	WatchersCount   int
	PrimaryLanguage *string
	Topics          []string
	CreatedAt       *time.Time
	UpdatedAt       *time.Time
	PushedAt        *time.Time
}

// githubReleasesSummary captures aggregate release info used for enrichment.
type githubReleasesSummary struct {
	TotalDownloads    int
	LatestPublishedAt *time.Time
}

// isSemverVersion returns true if the version string appears to follow SemVer (allows optional leading 'v').
func isSemverVersion(v string) bool {
	v = strings.TrimSpace(v)
	semverRe := regexp.MustCompile(`^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	return semverRe.MatchString(v)
}

// timePtrToRFC3339 formats a *time.Time as RFC3339 or returns nil if the pointer is nil.
func timePtrToRFC3339(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

// fetchGitHubTopics returns repository topics using the dedicated endpoint.
func (s *Service) fetchGitHubTopics(ctx context.Context, owner, repo string) ([]string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/topics", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if s.githubToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.githubToken)
	}
	// Topics historically required a preview Accept; modern API returns with standard as well.
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return []string{}, nil
	}
	var payload struct {
		Names []string `json:"names"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Names == nil {
		payload.Names = []string{}
	}
	return payload.Names, nil
}

// fetchGitHubTags returns up to 'limit' git tag names.
func (s *Service) fetchGitHubTags(ctx context.Context, owner, repo string, limit int) ([]string, error) {
	tags := []string{}
	page := 1
	for len(tags) < limit {
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/tags?per_page=100&page=%d", owner, repo, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return tags, err
		}
		if s.githubToken != "" {
			req.Header.Set("Authorization", "Bearer "+s.githubToken)
		}
		if req.Header.Get("Accept") == "" {
			req.Header.Set("Accept", "application/vnd.github+json")
		}
		client := s.httpClient
		if client == nil {
			client = http.DefaultClient
		}
		resp, err := client.Do(req)
		if err != nil {
			return tags, err
		}
		var payload []struct {
			Name string `json:"name"`
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			break
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			_ = resp.Body.Close()
			return tags, err
		}
		_ = resp.Body.Close()
		if len(payload) == 0 {
			break
		}
		for _, t := range payload {
			tags = append(tags, t.Name)
			if len(tags) >= limit {
				break
			}
		}
		page++
	}
	return tags, nil
}

// fetchGitHubOrgIsVerified returns true if the owner is an org and it is verified.
func (s *Service) fetchGitHubOrgIsVerified(ctx context.Context, owner string) (bool, error) {
	// Call orgs endpoint; if 404, assume it's a user (not org) → false.
	url := fmt.Sprintf("https://api.github.com/orgs/%s", owner)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	if s.githubToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.githubToken)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, nil
	}
	var payload struct {
		IsVerified bool `json:"is_verified"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return false, err
	}
	return payload.IsVerified, nil
}

// detectDependabotEnabled checks for the presence of .github/dependabot.yml
func (s *Service) detectDependabotEnabled(ctx context.Context, owner, repo string) (bool, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/.github/dependabot.yml", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	if s.githubToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.githubToken)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		return true, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	return false, nil
}

// detectCodeQLEnabled scans up to N workflow files for 'codeql' usage.
func (s *Service) detectCodeQLEnabled(ctx context.Context, owner, repo string) (bool, error) {
	dirURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/.github/workflows", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dirURL, nil)
	if err != nil {
		return false, err
	}
	if s.githubToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.githubToken)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return false, nil
	}
	var entries []struct {
		Name        string `json:"name"`
		Path        string `json:"path"`
		DownloadURL string `json:"download_url"`
		Type        string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		_ = resp.Body.Close()
		return false, err
	}
	_ = resp.Body.Close()
	maxFiles := 10
	count := 0
	for _, e := range entries {
		if e.Type != "file" {
			continue
		}
		count++
		if count > maxFiles {
			break
		}
		// Prefer download_url to get raw content easily
		fileURL := e.DownloadURL
		if fileURL == "" {
			// fallback to content endpoint
			fileURL = fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/HEAD/%s", owner, repo, url.PathEscape(e.Path))
		}
		creq, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
		if err != nil {
			continue
		}
		if s.githubToken != "" {
			creq.Header.Set("Authorization", "Bearer "+s.githubToken)
		}
		cclient := s.httpClient
		if cclient == nil {
			cclient = http.DefaultClient
		}
		cresp, err := cclient.Do(creq)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(cresp.Body)
		_ = cresp.Body.Close()
		content := strings.ToLower(string(body))
		if strings.Contains(content, "github/codeql-action") || strings.Contains(content, "codeql") {
			return true, nil
		}
	}
	return false, nil
}

// probeEndpointHealth performs a short HTTP GET to the given URL.
// Any HTTP response (2xx-5xx or 401) counts as reachable; network errors/timeouts are unreachable.
func probeEndpointHealth(ctx context.Context, rawURL string) (bool, *int, *time.Time) {
	// Validate URL
	if _, err := url.ParseRequestURI(rawURL); err != nil {
		return false, nil, nil
	}
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return false, nil, nil
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return false, nil, nil
	}
	_ = resp.Body.Close()
	elapsed := int(time.Since(start).Milliseconds())
	now := time.Now().UTC()
	return true, &elapsed, &now
}

// fetchOpenSSFScore retrieves the OpenSSF Scorecard score (0-10) for a GitHub repo.
func (s *Service) fetchOpenSSFScore(ctx context.Context, owner, repo string) (float64, error) {
	url := fmt.Sprintf("https://api.securityscorecards.dev/projects/github.com/%s/%s", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, nil
	}
	var payload struct {
		Score float64 `json:"score"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, err
	}
	return payload.Score, nil
}

// fetchDependabotAlertsCount returns total count of Dependabot alerts using Link header pagination.
func (s *Service) fetchDependabotAlertsCount(ctx context.Context, owner, repo string) (*int, error) {
	if strings.TrimSpace(s.githubToken) == "" {
		return nil, nil
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/dependabot/alerts?per_page=1", owner, repo)
	return s.fetchAlertCountFromLink(ctx, url)
}

// fetchCodeScanningAlertsCount returns total count of Code Scanning alerts using Link header pagination.
func (s *Service) fetchCodeScanningAlertsCount(ctx context.Context, owner, repo string) (*int, error) {
	if strings.TrimSpace(s.githubToken) == "" {
		return nil, nil
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/code-scanning/alerts?per_page=1", owner, repo)
	return s.fetchAlertCountFromLink(ctx, url)
}

// fetchAlertCountFromLink performs a single-page request with per_page=1 and derives count from Link or body length.
func (s *Service) fetchAlertCountFromLink(ctx context.Context, rawURL string) (*int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	// requires token with security_events to access alerts endpoints
	if s.githubToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.githubToken)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	// If unauthorized/forbidden/not found, treat as unavailable
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("alerts api status %d", resp.StatusCode)
	}
	link := resp.Header.Get("Link")
	if link != "" {
		if last, ok := parseLastPageFromLink(link); ok {
			return &last, nil
		}
	}
	// Fallback: count array length (0 or 1 since per_page=1)
	var arr []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
		return nil, err
	}
	n := len(arr)
	return &n, nil
}

// parseLastPageFromLink extracts the last page number from a GitHub Link header.
func parseLastPageFromLink(link string) (int, bool) {
	// Example: <https://api.github.com/...&page=3>; rel="last", <...&page=1>; rel="first"
	re := regexp.MustCompile(`<([^>]+)>;\s*rel="last"`)
	m := re.FindStringSubmatch(link)
	if len(m) != 2 {
		return 0, false
	}
	u, err := url.Parse(m[1])
	if err != nil {
		return 0, false
	}
	pageStr := u.Query().Get("page")
	if pageStr == "" {
		return 0, false
	}
	n, err := strconv.Atoi(pageStr)
	if err != nil {
		return 0, false
	}
	return n, true
}
