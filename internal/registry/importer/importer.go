package importer

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	"github.com/agentregistry-dev/agentregistry/internal/registry/seed"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/internal/registry/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// Service handles importing seed data into the registry
type Service struct {
	registry            service.RegistryService
	httpClient          *http.Client
	requestHeaders      map[string]string
	updateIfExists      bool
	githubToken         string
	readmeSeedPath      string
	progressCachePath   string
	progressMu          sync.RWMutex
	processedServers    map[string]struct{}
	generateEmbeddings  bool
	embeddingProvider   embeddings.Provider
	embeddingDimensions int
}

// NewService creates a new importer service with sane defaults
func NewService(registry service.RegistryService) *Service {
	// Allow user to override HTTP timeout via environment variable (seconds)
	timeout := 30 * time.Second
	if s := strings.TrimSpace(os.Getenv("AR_HTTP_TIMEOUT_SECONDS")); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 && v <= 120 {
			timeout = time.Duration(v) * time.Second
		}
	}
	return &Service{
		registry:         registry,
		httpClient:       &http.Client{Timeout: timeout},
		requestHeaders:   map[string]string{},
		processedServers: map[string]struct{}{},
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

// SetReadmeSeedPath configures an optional README seed file used for imports.
func (s *Service) SetReadmeSeedPath(path string) {
	s.readmeSeedPath = strings.TrimSpace(path)
}

// SetEmbeddingProvider configures the embedding provider used for semantic enrichment.
func (s *Service) SetEmbeddingProvider(provider embeddings.Provider) {
	s.embeddingProvider = provider
}

// SetEmbeddingDimensions sets the expected embedding dimensions for validation.
func (s *Service) SetEmbeddingDimensions(dimensions int) {
	s.embeddingDimensions = dimensions
}

// SetGenerateEmbeddings toggles whether embeddings should be generated during import.
func (s *Service) SetGenerateEmbeddings(enabled bool) {
	s.generateEmbeddings = enabled
}

// SetProgressCachePath configures a file used to persist import progress between runs.
func (s *Service) SetProgressCachePath(path string) {
	s.progressCachePath = strings.TrimSpace(path)
}

// ImportFromPath imports seed data from various sources:
// 1. Local file paths (*.json files) - expects ServerJSON array format
// 2. Direct HTTP URLs to seed.json files - expects ServerJSON array format
// 3. Registry API endpoints (e.g., /v0/servers, /v0.1/servers) - handles pagination automatically
func (s *Service) ImportFromPath(ctx context.Context, path string, enrichServerData bool) error {
	servers, err := s.readSeedFile(ctx, path)
	if err != nil {
		return fmt.Errorf("failed to read seed data: %w", err)
	}

	readmeSeeds, err := s.loadReadmeSeed(ctx)
	if err != nil {
		return err
	}

	if err := s.loadProgressCache(); err != nil {
		return fmt.Errorf("failed to load progress cache: %w", err)
	}

	if count := s.processedCount(); count > 0 {
		if s.progressCachePath != "" {
			log.Printf("Progress cache loaded: %d servers already processed", count)
		}
	}

	pending := make([]*apiv0.ServerJSON, 0, len(servers))
	for _, server := range servers {
		if s.isServerProcessed(server) {
			log.Printf("Skipping already processed server %s@%s", server.Name, server.Version)
			continue
		}
		pending = append(pending, server)
	}

	if len(pending) == 0 {
		log.Printf("All %d servers already processed; nothing to import", len(servers))
		return nil
	}

	// Import each server using registry service CreateServer
	total := len(pending)
	var processed int32

	wg := &sync.WaitGroup{}
	concurrencyLimit := 10
	sem := make(chan struct{}, concurrencyLimit)

	for _, server := range pending {
		srv := server
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() {
				<-sem
				wg.Done()
			}()

			current := atomic.AddInt32(&processed, 1)
			log.Printf("Importing %d/%d: %s@%s", current, total, srv.Name, srv.Version)
			s.importServer(ctx, srv, readmeSeeds, enrichServerData)
		}()
	}

	wg.Wait()

	return nil
}

func (s *Service) importServer(
	ctx context.Context,
	srv *apiv0.ServerJSON,
	readmeSeeds seed.ReadmeFile,
	enrichServerData bool,
) {
	if srv != nil {
		defer s.markServerProcessed(srv)
	}
	// check server json (schema validation) before attempting to enrich
	if err := validators.ValidateServerJSON(srv); err != nil {
		log.Printf("Skipping invalid server %s@%s: %v", srv.Name, srv.Version, err)
		return
	}

	// Best-effort enrichment
	if enrichServerData {
		if err := s.enrichServer(ctx, srv); err != nil {
			log.Printf("Warning: enrichment failed for %s@%s: %v", srv.Name, srv.Version, err)
		}
	}

	var embeddingRecord *database.SemanticEmbedding
	if s.generateEmbeddings && s.embeddingProvider != nil {
		if record, err := s.buildServerEmbedding(ctx, srv); err != nil {
			log.Printf("Warning: failed to generate embedding for %s@%s: %v", srv.Name, srv.Version, err)
		} else {
			embeddingRecord = record
		}
	}

	_, err := s.registry.CreateServer(ctx, srv)
	if err != nil {
		// If duplicate version and update is enabled, try update path
		if s.updateIfExists && errors.Is(err, database.ErrInvalidVersion) {
			if _, uerr := s.registry.UpdateServer(ctx, srv.Name, srv.Version, srv, nil); uerr != nil {
				log.Printf("Failed to update existing server %s: %v", srv.Name, uerr)
			} else {
				log.Printf("Updated existing server %s@%s", srv.Name, srv.Version)
			}
		} else {
			log.Printf("Failed to create server %s: %v", srv.Name, err)
			return
		}
	}

	if embeddingRecord != nil {
		if err := s.registry.UpsertServerEmbedding(ctx, srv.Name, srv.Version, embeddingRecord); err != nil {
			log.Printf("Warning: failed to store embedding for %s@%s: %v", srv.Name, srv.Version, err)
		}
	}

	if !enrichServerData {
		// Skip README fetch if enrichment is disabled
		return
	}
	readmeContent, readmeContentType := s.readmeFromSeed(readmeSeeds, srv)
	if len(readmeContent) == 0 {
		var readmeErr error
		readmeContent, readmeContentType, readmeErr = s.downloadReadme(ctx, srv)
		if readmeErr != nil {
			log.Printf("Warning: downloading README failed for %s@%s: %v", srv.Name, srv.Version, readmeErr)
		}
	}
	if len(readmeContent) > 0 {
		if err := s.registry.StoreServerReadme(ctx, srv.Name, srv.Version, readmeContent, readmeContentType); err != nil {
			log.Printf("Warning: storing README failed for %s@%s: %v", srv.Name, srv.Version, err)
		}
	}
}

func (s *Service) buildServerEmbedding(ctx context.Context, srv *apiv0.ServerJSON) (*database.SemanticEmbedding, error) {
	payload := embeddings.BuildServerEmbeddingPayload(srv)
	return embeddings.GenerateSemanticEmbedding(ctx, s.embeddingProvider, payload, s.embeddingDimensions)
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
	var dependabotAlerts any = nil
	var codeScanningAlerts any = nil
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

	var scorecardHighlights []string
	trimmedToken := strings.TrimSpace(s.githubToken)
	if score, highlights, err := runScorecardLibrary(ctx, owner, repo, trimmedToken); err == nil && score > 0 {
		ossfScore = score
		scorecardHighlights = highlights
	} else if score, err := runScorecardLocal(ctx, owner, repo); err == nil && score > 0 {
		ossfScore = score
	}

	// Additional scans
	dependencySummary, _ := s.fetchDependencyHealthSummary(ctx, owner, repo)
	containerSummary, _ := fetchDockerHubSummary(ctx, s.httpClient, owner, repo, server)

	// OSV vulnerability scan (npm, pip, go) via manifests at repo root
	osvRes, _ := s.runOSVScan(ctx, owner, repo)

	// Endpoint health probe (first remote only)
	var endpointReachable any = nil
	var endpointResponseMs any = nil
	var endpointCheckedAt any = nil
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
		server.Meta.PublisherProvided = map[string]any{}
	}

	enterprise := map[string]any{
		"stars": repoSummary.Stars,
		"downloads": map[string]any{
			"total": releasesSummary.TotalDownloads,
		},
		"score": score,
		"repo": map[string]any{
			"forks_count":      repoSummary.ForksCount,
			"watchers_count":   repoSummary.WatchersCount,
			"primary_language": repoSummary.PrimaryLanguage,
			"topics":           repoSummary.Topics,
			"tags":             repoTags,
		},
		"activity": map[string]any{
			"created_at": timePtrToRFC3339(repoSummary.CreatedAt),
			"updated_at": timePtrToRFC3339(repoSummary.UpdatedAt),
			"pushed_at":  timePtrToRFC3339(repoSummary.PushedAt),
		},
		"releases": map[string]any{
			"latest_published_at": timePtrToRFC3339(releasesSummary.LatestPublishedAt),
		},
		"identity": map[string]any{
			"publisher_identity_verified_by_jwt": false, // importer lacks JWT context
			"org_is_verified":                    orgIsVerified,
		},
		"semver": map[string]any{
			"uses_semver": usesSemver,
		},
		"scorecard": map[string]any{
			"openssf": ossfScore,
		},
		"endpoint_health": map[string]any{
			"reachable":       endpointReachable,
			"response_ms":     endpointResponseMs,
			"last_checked_at": endpointCheckedAt,
		},
		"security_scanning": map[string]any{
			"codeql_enabled":       codeqlEnabled,
			"dependabot_enabled":   dependabotEnabled,
			"code_scanning_alerts": codeScanningAlerts,
			"dependabot_alerts":    dependabotAlerts,
		},
		"scans": func() map[string]any {
			summaries := []string{}
			if osvRes != nil && strings.TrimSpace(osvRes.Summary) != "" {
				summaries = append(summaries, strings.TrimSpace(osvRes.Summary))
			}
			if text := dependencySummary.summaryString(); text != "" {
				summaries = append(summaries, text)
			}
			if text := containerSummary.summaryString(); text != "" {
				summaries = append(summaries, text)
			}
			var summaryValue any
			if len(summaries) > 0 {
				summaryValue = strings.Join(summaries, " | ")
			} else {
				summaryValue = nil
			}
			details := []any{}
			addDetail := func(text string) {
				if text == "" {
					return
				}
				if len(details) >= 50 {
					return
				}
				details = append(details, text)
			}
			for _, d := range scorecardHighlights {
				addDetail(d)
			}
			addDetail(dependencySummary.detailString())
			addDetail(containerSummary.detailString())
			if osvRes != nil {
				for _, d := range osvRes.Details {
					addDetail(d)
				}
			}
			return map[string]any{
				"summary": summaryValue,
				"details": details,
				"dependency_health": func() any {
					if dependencySummary == nil {
						return nil
					}
					return map[string]any{
						"packages_total":    dependencySummary.TotalPackages,
						"ecosystems":        dependencySummary.Ecosystems,
						"copyleft_licenses": dependencySummary.CopyleftCount,
						"unknown_licenses":  dependencySummary.UnknownLicenseCount,
					}
				}(),
				"container_images": func() any {
					if containerSummary == nil {
						return []any{}
					}
					entry := map[string]any{
						"registry":              containerSummary.Registry,
						"image":                 containerSummary.Image,
						"pull_count":            containerSummary.PullCount,
						"star_count":            containerSummary.StarCount,
						"last_updated_at":       timePtrToRFC3339(containerSummary.LastUpdatedAt),
						"latest_tag":            containerSummary.LatestTag,
						"latest_tag_updated_at": timePtrToRFC3339(containerSummary.LatestTagUpdatedAt),
					}
					return []any{entry}
				}(),
			}
		}(),
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
// func (s *Service) fetchGitHubStars(ctx context.Context, owner, repo string) (int, error) {
// 	url := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)
// 	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
// 	if err != nil {
// 		return 0, err
// 	}
// 	// Do NOT forward arbitrary registry headers to GitHub.
// 	// Only apply an explicit GitHub token if provided.
// 	if s.githubToken != "" {
// 		req.Header.Set("Authorization", "Bearer "+s.githubToken)
// 	}
// 	if req.Header.Get("Accept") == "" {
// 		req.Header.Set("Accept", "application/vnd.github+json")
// 	}
// 	client := s.httpClient
// 	if client == nil {
// 		client = http.DefaultClient
// 	}
// 	resp, err := client.Do(req)
// 	if err != nil {
// 		return 0, err
// 	}
// 	defer func() { _ = resp.Body.Close() }()
// 	if resp.StatusCode != http.StatusOK {
// 		body, _ := io.ReadAll(resp.Body)
// 		return 0, fmt.Errorf("github api status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
// 	}
// 	var payload struct {
// 		Stars int `json:"stargazers_count"`
// 	}
// 	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
// 		return 0, err
// 	}
// 	return payload.Stars, nil
// }

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
func timePtrToRFC3339(t *time.Time) any {
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

func (s *Service) fetchRepoContentFile(ctx context.Context, owner, repo, path string) ([]byte, error) {
	return s.fetchRepoContentFileWithRename(ctx, owner, repo, path, true)
}

func (s *Service) fetchRepoContentFileWithRename(ctx context.Context, owner, repo, path string, allowRename bool) ([]byte, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", owner, repo, path)
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
		if allowRename {
			if newOwner, newRepo, renamed, renameErr := s.resolveRenamedRepo(ctx, owner, repo); renameErr != nil {
				return nil, fmt.Errorf("content %s status %d (repo lookup failed: %w)", path, resp.StatusCode, renameErr)
			} else if renamed {
				log.Printf("Detected GitHub repository rename: %s/%s -> %s/%s", owner, repo, newOwner, newRepo)
				return s.fetchRepoContentFileWithRename(ctx, newOwner, newRepo, path, false)
			}
		}
		return nil, fmt.Errorf("content %s status %d", path, resp.StatusCode)
	}
	var payload struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if strings.ToLower(payload.Encoding) == "base64" {
		data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(payload.Content, "\n", ""))
		if err != nil {
			return nil, err
		}
		return data, nil
	}
	// fallback: sometimes API may return raw
	body, _ := io.ReadAll(resp.Body)
	return body, nil
}

func (s *Service) downloadReadme(ctx context.Context, server *apiv0.ServerJSON) ([]byte, string, error) {
	if server.Repository == nil || server.Repository.URL == "" {
		return nil, "", nil
	}
	owner, repo := parseGitHubRepo(server.Repository.URL)
	if owner == "" || repo == "" {
		return nil, "", nil
	}

	content, err := s.fetchRepoContentFile(ctx, owner, repo, "README.md")
	if err != nil {
		if strings.Contains(err.Error(), "status 404") {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("failed to fetch README.md: %w", err)
	}

	return content, "text/markdown", nil
}

func (s *Service) resolveRenamedRepo(ctx context.Context, owner, repo string) (string, string, bool, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", false, err
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
		return "", "", false, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", "", false, nil
	}

	var payload struct {
		FullName string `json:"full_name"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", "", false, err
	}

	if payload.FullName == "" {
		return "", "", false, nil
	}

	parts := strings.Split(payload.FullName, "/")
	if len(parts) != 2 {
		return "", "", false, nil
	}

	newOwner := parts[0]
	newRepo := parts[1]

	if strings.EqualFold(owner, newOwner) && strings.EqualFold(repo, newRepo) {
		return "", "", false, nil
	}

	return newOwner, newRepo, true, nil
}

func (s *Service) loadReadmeSeed(ctx context.Context) (seed.ReadmeFile, error) {
	if strings.TrimSpace(s.readmeSeedPath) == "" {
		return nil, nil
	}

	var data []byte
	var err error
	if strings.HasPrefix(s.readmeSeedPath, "http://") || strings.HasPrefix(s.readmeSeedPath, "https://") {
		data, err = s.fetchFromHTTP(ctx, s.readmeSeedPath)
	} else {
		data, err = os.ReadFile(s.readmeSeedPath)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read README seed data from %s: %w", s.readmeSeedPath, err)
	}

	if len(data) == 0 {
		return seed.ReadmeFile{}, nil
	}

	var readmes seed.ReadmeFile
	if err := json.Unmarshal(data, &readmes); err != nil {
		return nil, fmt.Errorf("failed to parse README seed data: %w", err)
	}

	return readmes, nil
}

func (s *Service) readmeFromSeed(readmes seed.ReadmeFile, server *apiv0.ServerJSON) ([]byte, string) {
	if readmes == nil {
		return nil, ""
	}
	entry, ok := readmes[seed.Key(server.Name, server.Version)]
	if !ok {
		return nil, ""
	}

	content, contentType, err := entry.Decode()
	if err != nil {
		log.Printf("Warning: invalid README seed for %s@%s: %v", server.Name, server.Version, err)
		return nil, ""
	}
	return content, contentType
}

func (s *Service) loadProgressCache() error {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()

	s.processedServers = make(map[string]struct{})

	if strings.TrimSpace(s.progressCachePath) == "" {
		return nil
	}

	data, err := os.ReadFile(s.progressCachePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		key := strings.TrimSpace(line)
		if key == "" {
			continue
		}
		s.processedServers[key] = struct{}{}
	}

	return nil
}

func (s *Service) processedCount() int {
	s.progressMu.RLock()
	defer s.progressMu.RUnlock()

	return len(s.processedServers)
}

func (s *Service) isServerProcessed(server *apiv0.ServerJSON) bool {
	key := s.progressCacheKey(server)
	if key == "" {
		return false
	}

	s.progressMu.RLock()
	defer s.progressMu.RUnlock()

	_, ok := s.processedServers[key]
	return ok
}

func (s *Service) markServerProcessed(server *apiv0.ServerJSON) {
	key := s.progressCacheKey(server)
	if key == "" {
		return
	}

	s.progressMu.Lock()
	defer s.progressMu.Unlock()

	if s.processedServers == nil {
		s.processedServers = make(map[string]struct{})
	}

	if _, exists := s.processedServers[key]; exists {
		return
	}

	s.processedServers[key] = struct{}{}

	if strings.TrimSpace(s.progressCachePath) == "" {
		return
	}

	if err := s.persistProgressCacheLocked(); err != nil {
		log.Printf("Warning: failed to persist progress cache: %v", err)
	}
}

func (s *Service) persistProgressCacheLocked() error {
	if strings.TrimSpace(s.progressCachePath) == "" {
		return nil
	}

	dir := filepath.Dir(s.progressCachePath)
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(s.progressCachePath)+".tmp-*")
	if err != nil {
		return err
	}

	keys := make([]string, 0, len(s.processedServers))
	for key := range s.processedServers {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	for _, key := range keys {
		if _, err := tmp.WriteString(key + "\n"); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
			return err
		}
	}

	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}

	if err := os.Rename(tmp.Name(), s.progressCachePath); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}

	return nil
}

func (s *Service) progressCacheKey(server *apiv0.ServerJSON) string {
	if server == nil {
		return ""
	}

	name := strings.TrimSpace(server.Name)
	version := strings.TrimSpace(server.Version)
	if name == "" || version == "" {
		return ""
	}

	return seed.Key(name, version)
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

// runScorecardLocal invokes the Scorecard CLI against the repo remotely and parses JSON output.
// It is best-effort and returns 0 if unavailable. It uses a short timeout.
func runScorecardLocal(ctx context.Context, owner, repo string) (float64, error) {
	// Check presence
	if _, err := exec.LookPath("scorecard"); err != nil {
		return 0, err
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	// Use remote mode to avoid local clone cost
	cmd := exec.CommandContext(cctx, "scorecard", "--repo=github.com/"+owner+"/"+repo, "--format=json")
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("scorecard: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	// Parse JSON { "score": number, ... }
	var payload struct {
		Score float64 `json:"score"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		return 0, err
	}
	return payload.Score, nil
}
