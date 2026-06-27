package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	configPath       = "readme.config.json"
	readmePath       = "README.md"
	assetDir         = "assets/readme"
	iconRegistryPath = "assets/readme/icons.json"
	startMarker      = "<!-- README-ASSETS:START -->"
	endMarker        = "<!-- README-ASSETS:END -->"
	githubAPIURL     = "https://api.github.com"
	githubGraphQLURL = "https://api.github.com/graphql"
)

type config struct {
	GitHubUsername        string       `json:"githubUsername"`
	BlogURL               string       `json:"blogURL"`
	RSSURL                string       `json:"rssURL"`
	Bio                   []string     `json:"bio"`
	Focus                 []string     `json:"focus"`
	Links                 []link       `json:"links"`
	Environment           []configItem `json:"environment"`
	Stack                 []stackGroup `json:"stack"`
	HighlightProjects     []string     `json:"highlightProjects"`
	BlogPostLimit         int          `json:"blogPostLimit"`
	RecentRepositoryLimit int          `json:"recentRepositoryLimit"`
	ActivityLimit         int          `json:"activityLimit"`
}

type link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type configItem struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type stackGroup struct {
	Label string   `json:"label"`
	Items []string `json:"items"`
}

type githubProfile struct {
	Login       string `json:"login"`
	Name        string `json:"name"`
	Location    string `json:"location"`
	PublicRepos int    `json:"public_repos"`
	Followers   int    `json:"followers"`
}

type repository struct {
	Name        string    `json:"name"`
	FullName    string    `json:"full_name"`
	Description string    `json:"description"`
	HTMLURL     string    `json:"html_url"`
	Language    string    `json:"language"`
	Stars       int       `json:"stargazers_count"`
	Forks       int       `json:"forks_count"`
	Fork        bool      `json:"fork"`
	Archived    bool      `json:"archived"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type githubEvent struct {
	Type      string          `json:"type"`
	Repo      eventRepository `json:"repo"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

type eventRepository struct {
	Name string `json:"name"`
}

type eventPayload struct {
	Action  string `json:"action"`
	RefType string `json:"ref_type"`
	Size    int    `json:"size"`
}

type blogFeed struct {
	Channel struct {
		Items []blogPost `xml:"item"`
	} `xml:"channel"`
}

type blogPost struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	Published   string `xml:"pubDate"`
}

type githubData struct {
	Profile githubProfile
	Repos   []repository
}

type contributionStats struct {
	Ready                          bool
	PrivateMode                    string
	LineMode                       string
	Note                           string
	StartedYear                    int
	EndedYear                      int
	TotalContributions             int
	TotalCommitContributions       int
	TotalIssueContributions        int
	TotalPullRequestContribs       int
	TotalPullRequestReviewContribs int
	TotalRepositoryContribs        int
	RestrictedContributions        int
	HasRestrictedContributions     bool
	RepositoriesWithCommits        int
	Years                          []yearContributionStats
	Lines                          contributionLineStats
}

type yearContributionStats struct {
	Year          int
	Commits       int
	Contributions int
}

type contributionLineStats struct {
	Available    bool
	Partial      bool
	Additions    int
	Deletions    int
	PullRequests int
}

type svgDocument struct {
	Name    string
	Content []byte
	Dynamic bool
	Ready   bool
}

type githubClient struct {
	http  *http.Client
	token string
}

type graphQLClient struct {
	http  *http.Client
	token string
}

type svgBuilder struct {
	bytes.Buffer
	width  int
	height int
}

type svgTheme struct {
	Background   string
	Surface      string
	SurfaceAlt   string
	Border       string
	Accent       string
	AccentStrong string
	AccentSoft   string
	ActivityLow  string
	ActivityMid  string
	Text         string
	Muted        string
	Dim          string
	Success      string
	Warning      string
	Danger       string
}

type icon struct {
	Name    string     `json:"name"`
	ViewBox string     `json:"viewBox"`
	Paths   []iconPath `json:"paths"`
	Source  string     `json:"source"`
	License string     `json:"license"`
}

type iconPath struct {
	D string `json:"d"`
}

type iconRegistryFile struct {
	Icons map[string]icon `json:"icons"`
}

type iconViewBox struct {
	MinX   float64
	MinY   float64
	Width  float64
	Height float64
}

var readmeTheme = svgTheme{
	Background:   "#0d1117",
	Surface:      "#111827",
	SurfaceAlt:   "#172033",
	Border:       "#263244",
	Accent:       "#38bdf8",
	AccentStrong: "#0ea5e9",
	AccentSoft:   "#7dd3fc",
	ActivityLow:  "#164e63",
	ActivityMid:  "#0369a1",
	Text:         "#dbeafe",
	Muted:        "#93a4b8",
	Dim:          "#64748b",
	Success:      "#22c55e",
	Warning:      "#facc15",
	Danger:       "#f87171",
}

var iconRegistry map[string]icon

func main() {
	if err := run(); err != nil {
		log.Fatalf("readme generator: %v", err)
	}
}

func run() error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	iconRegistry, err = loadIconRegistry(iconRegistryPath)
	if err != nil {
		return err
	}
	if err := validateConfiguredIcons(cfg, iconRegistry); err != nil {
		return err
	}
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		return fmt.Errorf("create asset directory: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()

	client := &githubClient{
		http:  &http.Client{Timeout: 25 * time.Second},
		token: strings.TrimSpace(os.Getenv("GH_TOKEN")),
	}

	var gh githubData
	profileErr := client.get(ctx, "/users/"+cfg.GitHubUsername, &gh.Profile)
	if profileErr != nil {
		log.Printf("GitHub profile unavailable: %v", profileErr)
	} else {
		log.Printf("GitHub profile loaded: %s", gh.Profile.Login)
	}

	repos, reposErr := client.repositories(ctx, cfg.GitHubUsername)
	if reposErr != nil {
		log.Printf("GitHub repositories unavailable: %v", reposErr)
	} else {
		gh.Repos = repos
		log.Printf("GitHub repositories loaded: %d", len(repos))
	}

	events, eventsErr := client.events(ctx, cfg.GitHubUsername)
	if eventsErr != nil {
		log.Printf("GitHub activity unavailable: %v", eventsErr)
	} else {
		log.Printf("GitHub public events loaded: %d", len(events))
	}

	posts, blogErr := fetchBlog(ctx, client.http, cfg.RSSURL, cfg.BlogPostLimit)
	if blogErr != nil {
		log.Printf("Blog feed unavailable: %v", blogErr)
	} else {
		log.Printf("Blog posts loaded: %d", len(posts))
	}

	contributions, contributionsErr := fetchContributionStats(ctx, client.http, cfg)
	if contributionsErr != nil {
		log.Printf("GitHub contribution stats unavailable: %v", contributionsErr)
	} else if !contributions.Ready {
		log.Printf("GitHub contribution stats rendered in fallback mode")
	} else {
		log.Printf("GitHub contribution stats loaded: %d year(s), %d contribution(s)", len(contributions.Years), contributions.TotalContributions)
	}

	githubReady := profileErr == nil && reposErr == nil
	documents := []svgDocument{
		{Name: "header.svg", Content: renderHeader(cfg, gh.Profile, profileErr == nil), Dynamic: true, Ready: profileErr == nil},
		{Name: "about.svg", Content: renderAbout(cfg), Ready: true},
		{Name: "config.svg", Content: renderConfig(cfg), Ready: true},
		{Name: "stack.svg", Content: renderStack(cfg), Ready: true},
		{Name: "blog.svg", Content: renderBlog(cfg, posts, blogErr == nil), Dynamic: true, Ready: blogErr == nil},
		{Name: "github-stats.svg", Content: renderGitHubStats(cfg, gh, githubReady), Dynamic: true, Ready: githubReady},
		{Name: "activity.svg", Content: renderActivity(cfg, events, eventsErr == nil), Dynamic: true, Ready: eventsErr == nil},
		{Name: "contributions.svg", Content: renderContributionStats(contributions), Dynamic: true, Ready: contributionsErr == nil},
	}

	for _, document := range documents {
		path := filepath.Join(assetDir, document.Name)
		if document.Dynamic && !document.Ready && fileExists(path) {
			log.Printf("Keeping previous %s because fresh data is unavailable", path)
			continue
		}
		if err := writeSVG(path, document.Content); err != nil {
			return err
		}
	}

	if err := updateReadme(cfg); err != nil {
		return err
	}
	log.Printf("README assets are up to date")
	return nil
}

func loadConfig(path string) (config, error) {
	var cfg config
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	if cfg.GitHubUsername == "" || cfg.BlogURL == "" || cfg.RSSURL == "" {
		return cfg, errors.New("config requires githubUsername, blogURL, and rssURL")
	}
	if len(cfg.Links) == 0 || cfg.Links[0].URL == "" {
		return cfg, errors.New("config requires at least one personal link")
	}
	if cfg.BlogPostLimit <= 0 {
		cfg.BlogPostLimit = 3
	}
	if cfg.RecentRepositoryLimit <= 0 {
		cfg.RecentRepositoryLimit = 3
	}
	if cfg.ActivityLimit <= 0 {
		cfg.ActivityLimit = 5
	}
	return cfg, nil
}

func loadIconRegistry(path string) (map[string]icon, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read icon registry: %w", err)
	}
	var registry iconRegistryFile
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&registry); err != nil {
		return nil, fmt.Errorf("parse icon registry: %w", err)
	}
	if len(registry.Icons) == 0 {
		return nil, errors.New("icon registry is empty")
	}
	for key, icon := range registry.Icons {
		if strings.TrimSpace(icon.ViewBox) == "" || len(icon.Paths) == 0 {
			return nil, fmt.Errorf("icon %q has no viewBox or path data", key)
		}
		if _, err := parseViewBox(icon.ViewBox); err != nil {
			return nil, fmt.Errorf("icon %q: %w", key, err)
		}
	}
	return registry.Icons, nil
}

func validateConfiguredIcons(cfg config, registry map[string]icon) error {
	var missing []string
	seen := map[string]bool{}
	check := func(name string) {
		key := strings.ToLower(strings.TrimSpace(name))
		if _, ok := registry[key]; !ok && !seen[key] {
			seen[key] = true
			missing = append(missing, name)
		}
	}
	for _, item := range cfg.Environment {
		check(item.Value)
	}
	for _, group := range cfg.Stack {
		for _, item := range group.Items {
			check(item)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("icon registry is missing: %s", strings.Join(missing, ", "))
	}
	return nil
}

func (client *githubClient) get(ctx context.Context, path string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAPIURL+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "kristyancarvalho-readme-generator")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if client.token != "" {
		request.Header.Set("Authorization", "Bearer "+client.token)
	}

	response, err := client.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("%s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (client *githubClient) repositories(ctx context.Context, username string) ([]repository, error) {
	var all []repository
	for page := 1; ; page++ {
		var batch []repository
		path := fmt.Sprintf("/users/%s/repos?type=public&sort=updated&per_page=100&page=%d", username, page)
		if err := client.get(ctx, path, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			break
		}
	}
	return all, nil
}

func (client *githubClient) events(ctx context.Context, username string) ([]githubEvent, error) {
	var events []githubEvent
	if err := client.get(ctx, "/users/"+username+"/events/public?per_page=100", &events); err != nil {
		return nil, err
	}
	return events, nil
}

func fetchContributionStats(ctx context.Context, httpClient *http.Client, cfg config) (contributionStats, error) {
	profileToken := strings.TrimSpace(os.Getenv("PROFILE_STATS_TOKEN"))
	token := profileToken
	privateHint := token != ""
	if token == "" {
		token = strings.TrimSpace(os.Getenv("GH_TOKEN"))
	}
	if token == "" {
		return fallbackContributionStats("token ausente", "indisponivel"), nil
	}

	client := &graphQLClient{
		http:  httpClient,
		token: token,
	}
	years, viewerLogin, err := client.contributionYears(ctx, cfg.GitHubUsername)
	if err != nil {
		return fallbackContributionStats("api indisponivel", "indisponivel"), err
	}
	if len(years) == 0 {
		years = []int{time.Now().UTC().Year()}
	}
	sort.Ints(years)

	stats := contributionStats{
		Ready:       true,
		PrivateMode: contributionPrivateMode(privateHint, viewerLogin, cfg.GitHubUsername),
		LineMode:    "indisponivel",
		StartedYear: years[0],
		EndedYear:   years[len(years)-1],
	}

	now := time.Now().UTC()
	for _, year := range years {
		if year > now.Year() {
			continue
		}
		yearStats, err := client.contributionYear(ctx, cfg.GitHubUsername, year, now)
		if err != nil {
			if stats.TotalContributions == 0 && stats.TotalCommitContributions == 0 {
				return fallbackContributionStats("api indisponivel", "indisponivel"), err
			}
			stats.Note = "parcial"
			break
		}
		stats.TotalContributions += yearStats.TotalContributions
		stats.TotalCommitContributions += yearStats.TotalCommitContributions
		stats.TotalIssueContributions += yearStats.TotalIssueContributions
		stats.TotalPullRequestContribs += yearStats.TotalPullRequestContribs
		stats.TotalPullRequestReviewContribs += yearStats.TotalPullRequestReviewContribs
		stats.TotalRepositoryContribs += yearStats.TotalRepositoryContribs
		stats.RestrictedContributions += yearStats.RestrictedContributions
		stats.RepositoriesWithCommits += yearStats.RepositoriesWithCommits
		stats.HasRestrictedContributions = stats.HasRestrictedContributions || yearStats.HasRestrictedContributions
		stats.Years = append(stats.Years, yearContributionStats{
			Year:          year,
			Commits:       yearStats.TotalCommitContributions,
			Contributions: yearStats.TotalContributions,
		})
	}

	if privateHint && strings.EqualFold(viewerLogin, cfg.GitHubUsername) {
		lines, err := client.pullRequestLineStats(ctx, cfg.GitHubUsername, years)
		if err == nil {
			stats.Lines = lines
			if lines.Available {
				stats.LineMode = "prs acessiveis"
				if lines.Partial {
					stats.LineMode = "parcial"
				}
			}
		} else {
			stats.LineMode = "indisponivel"
		}
	}

	if len(stats.Years) == 0 {
		stats.Note = "sem atividade"
	}
	return stats, nil
}

func fallbackContributionStats(privateMode, lineMode string) contributionStats {
	return contributionStats{
		PrivateMode: privateMode,
		LineMode:    lineMode,
		Note:        "Configure PROFILE_STATS_TOKEN para dados privados agregados.",
	}
}

func contributionPrivateMode(privateHint bool, viewerLogin, username string) string {
	if !privateHint {
		return "somente publico"
	}
	if strings.EqualFold(viewerLogin, username) {
		return "agregado"
	}
	return "limitado"
}

type contributionYearsResponse struct {
	Viewer struct {
		Login string `json:"login"`
	} `json:"viewer"`
	User *struct {
		ContributionsCollection struct {
			ContributionYears []int `json:"contributionYears"`
		} `json:"contributionsCollection"`
	} `json:"user"`
}

type contributionYearResponse struct {
	User *struct {
		ContributionsCollection struct {
			ContributionCalendar struct {
				TotalContributions int `json:"totalContributions"`
			} `json:"contributionCalendar"`
			TotalCommitContributions                int  `json:"totalCommitContributions"`
			TotalIssueContributions                 int  `json:"totalIssueContributions"`
			TotalPullRequestContribs                int  `json:"totalPullRequestContributions"`
			TotalPullRequestReviewContribs          int  `json:"totalPullRequestReviewContributions"`
			TotalRepositoryContribs                 int  `json:"totalRepositoryContributions"`
			TotalRepositoriesWithContributedCommits int  `json:"totalRepositoriesWithContributedCommits"`
			RestrictedContributions                 int  `json:"restrictedContributionsCount"`
			HasRestrictedContributions              bool `json:"hasAnyRestrictedContributions"`
		} `json:"contributionsCollection"`
	} `json:"user"`
}

type pullRequestLineSearchResponse struct {
	Search struct {
		IssueCount int `json:"issueCount"`
		PageInfo   struct {
			HasNextPage bool   `json:"hasNextPage"`
			EndCursor   string `json:"endCursor"`
		} `json:"pageInfo"`
		Nodes []struct {
			Additions int `json:"additions"`
			Deletions int `json:"deletions"`
		} `json:"nodes"`
	} `json:"search"`
}

func (client *graphQLClient) contributionYears(ctx context.Context, username string) ([]int, string, error) {
	const query = `
query($login: String!) {
  viewer {
    login
  }
  user(login: $login) {
    contributionsCollection {
      contributionYears
    }
  }
}`
	var response contributionYearsResponse
	if err := client.query(ctx, query, map[string]any{"login": username}, &response); err != nil {
		return nil, "", err
	}
	if response.User == nil {
		return nil, response.Viewer.Login, errors.New("GitHub user not found")
	}
	return response.User.ContributionsCollection.ContributionYears, response.Viewer.Login, nil
}

func (client *graphQLClient) contributionYear(ctx context.Context, username string, year int, now time.Time) (contributionStats, error) {
	const query = `
query($login: String!, $from: DateTime!, $to: DateTime!) {
  user(login: $login) {
    contributionsCollection(from: $from, to: $to) {
      contributionCalendar {
        totalContributions
      }
      totalCommitContributions
      totalIssueContributions
      totalPullRequestContributions
      totalPullRequestReviewContributions
      totalRepositoryContributions
      totalRepositoriesWithContributedCommits
      restrictedContributionsCount
      hasAnyRestrictedContributions
    }
  }
}`
	from, to := contributionYearRange(year, now)
	var response contributionYearResponse
	err := client.query(ctx, query, map[string]any{
		"login": username,
		"from":  from.Format(time.RFC3339),
		"to":    to.Format(time.RFC3339),
	}, &response)
	if err != nil {
		return contributionStats{}, err
	}
	if response.User == nil {
		return contributionStats{}, errors.New("GitHub user not found")
	}
	collection := response.User.ContributionsCollection
	return contributionStats{
		TotalContributions:             collection.ContributionCalendar.TotalContributions,
		TotalCommitContributions:       collection.TotalCommitContributions,
		TotalIssueContributions:        collection.TotalIssueContributions,
		TotalPullRequestContribs:       collection.TotalPullRequestContribs,
		TotalPullRequestReviewContribs: collection.TotalPullRequestReviewContribs,
		TotalRepositoryContribs:        collection.TotalRepositoryContribs,
		RepositoriesWithCommits:        collection.TotalRepositoriesWithContributedCommits,
		RestrictedContributions:        collection.RestrictedContributions,
		HasRestrictedContributions:     collection.HasRestrictedContributions,
	}, nil
}

func contributionYearRange(year int, now time.Time) (time.Time, time.Time) {
	from := time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(year, time.December, 31, 23, 59, 59, 0, time.UTC)
	if year == now.Year() {
		to = now
	}
	return from, to
}

func (client *graphQLClient) pullRequestLineStats(ctx context.Context, username string, years []int) (contributionLineStats, error) {
	const query = `
query($query: String!, $cursor: String) {
  search(query: $query, type: ISSUE, first: 100, after: $cursor) {
    issueCount
    pageInfo {
      hasNextPage
      endCursor
    }
    nodes {
      ... on PullRequest {
        additions
        deletions
      }
    }
  }
}`
	// GitHub does not expose exact lifetime line totals in ContributionsCollection.
	// This intentionally omits repository, branch, title, URL, and path fields, and
	// sums only anonymous PR additions/deletions visible to PROFILE_STATS_TOKEN.
	stats := contributionLineStats{Available: true}
	nowYear := time.Now().UTC().Year()
	for _, year := range years {
		if year > nowYear {
			continue
		}
		searchQuery := fmt.Sprintf("author:%s type:pr created:%04d-01-01..%04d-12-31", username, year, year)
		var cursor any
		for page := 0; ; page++ {
			var response pullRequestLineSearchResponse
			err := client.query(ctx, query, map[string]any{
				"query":  searchQuery,
				"cursor": cursor,
			}, &response)
			if err != nil {
				return contributionLineStats{}, err
			}
			if response.Search.IssueCount > 1000 {
				stats.Partial = true
			}
			for _, node := range response.Search.Nodes {
				stats.Additions += node.Additions
				stats.Deletions += node.Deletions
				stats.PullRequests++
			}
			if !response.Search.PageInfo.HasNextPage || response.Search.PageInfo.EndCursor == "" {
				break
			}
			if page >= 9 {
				stats.Partial = true
				break
			}
			cursor = response.Search.PageInfo.EndCursor
		}
	}
	return stats, nil
}

func (client *graphQLClient) query(ctx context.Context, query string, variables map[string]any, target any) error {
	payload := struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}{
		Query:     query,
		Variables: variables,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode GraphQL request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, githubGraphQLURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "kristyancarvalho-readme-generator")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("Authorization", "Bearer "+client.token)

	response, err := client.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return graphQLHTTPError(response)
	}

	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Type string `json:"type"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&envelope); err != nil {
		return fmt.Errorf("decode GraphQL response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("GitHub GraphQL returned %d error(s)", len(envelope.Errors))
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return errors.New("GitHub GraphQL returned no data")
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		return fmt.Errorf("decode GraphQL data: %w", err)
	}
	return nil
}

func graphQLHTTPError(response *http.Response) error {
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return errors.New("GitHub GraphQL authentication failed")
	case http.StatusForbidden:
		if response.Header.Get("X-RateLimit-Remaining") == "0" {
			reset := formatRateLimitReset(response.Header.Get("X-RateLimit-Reset"))
			if reset != "" {
				return fmt.Errorf("GitHub GraphQL rate limit exceeded; resets at %s", reset)
			}
			return errors.New("GitHub GraphQL rate limit exceeded")
		}
		return errors.New("GitHub GraphQL access forbidden")
	default:
		return fmt.Errorf("GitHub GraphQL returned %s", response.Status)
	}
}

func formatRateLimitReset(value string) string {
	seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || seconds <= 0 {
		return ""
	}
	return time.Unix(seconds, 0).UTC().Format(time.RFC3339)
}

func fetchBlog(ctx context.Context, client *http.Client, url string, limit int) ([]blogPost, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "kristyancarvalho-readme-generator")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%s", response.Status)
	}
	var feed blogFeed
	if err := xml.NewDecoder(response.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("decode RSS: %w", err)
	}
	valid := make([]blogPost, 0, limit)
	for _, post := range feed.Channel.Items {
		post.Title = strings.TrimSpace(post.Title)
		post.Link = strings.TrimSpace(post.Link)
		post.Description = strings.TrimSpace(post.Description)
		if post.Title == "" || post.Link == "" {
			continue
		}
		valid = append(valid, post)
		if len(valid) == limit {
			break
		}
	}
	if len(valid) == 0 {
		return nil, errors.New("RSS feed has no valid posts")
	}
	return valid, nil
}

func newSVG(height int, title, description, section string) *svgBuilder {
	s := &svgBuilder{width: 960, height: height}
	fmt.Fprintf(&s.Buffer, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-labelledby="title desc">`, s.width, s.height, s.width, s.height)
	fmt.Fprintf(&s.Buffer, `<title id="title">%s</title><desc id="desc">%s</desc>`, esc(title), esc(description))
	fmt.Fprintf(&s.Buffer, `<style>
		.text{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;fill:%s}
		.muted{fill:%s}.dim{fill:%s}.accent{fill:%s}.strong{fill:%s}.soft{fill:%s}
		.label{font-size:15px;font-weight:700}.body{font-size:15px}.small{font-size:13px}
	</style>`, readmeTheme.Text, readmeTheme.Muted, readmeTheme.Dim, readmeTheme.Accent, readmeTheme.AccentStrong, readmeTheme.AccentSoft)
	fmt.Fprintf(&s.Buffer, `<rect width="960" height="100%%" rx="18" fill="%s"/>`, readmeTheme.Background)
	fmt.Fprintf(&s.Buffer, `<rect x="1" y="1" width="958" height="100%%" rx="17" fill="none" stroke="%s"/>`, readmeTheme.Border)
	fmt.Fprintf(&s.Buffer, `<circle cx="28" cy="28" r="5" fill="%s"/><circle cx="46" cy="28" r="5" fill="%s"/><circle cx="64" cy="28" r="5" fill="%s"/>`, readmeTheme.AccentStrong, readmeTheme.Accent, readmeTheme.AccentSoft)
	s.text(88, 34, "text label", section)
	s.line(20, 52, 940, 52, readmeTheme.Border, 1)
	return s
}

func (s *svgBuilder) finish() []byte {
	s.WriteString(`</svg>`)
	return s.Bytes()
}

func (s *svgBuilder) text(x, y int, class, value string) {
	fmt.Fprintf(&s.Buffer, `<text x="%d" y="%d" class="%s">%s</text>`, x, y, class, esc(value))
}

func (s *svgBuilder) rect(x, y, width, height, radius int, fill, stroke string) {
	fmt.Fprintf(&s.Buffer, `<rect x="%d" y="%d" width="%d" height="%d" rx="%d" fill="%s" stroke="%s"/>`, x, y, width, height, radius, fill, stroke)
}

func (s *svgBuilder) line(x1, y1, x2, y2 int, color string, width int) {
	fmt.Fprintf(&s.Buffer, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="%s" stroke-width="%d"/>`, x1, y1, x2, y2, color, width)
}

func (s *svgBuilder) pill(x, y int, value string, accent bool) int {
	width := runeLen(value)*8 + 24
	fill, stroke, class := readmeTheme.SurfaceAlt, readmeTheme.Border, "text small"
	if accent {
		fill, stroke, class = readmeTheme.Surface, readmeTheme.AccentStrong, "text small"
	}
	s.rect(x, y, width, 30, 8, fill, stroke)
	s.text(x+12, y+20, class, value)
	return width
}

func (s *svgBuilder) icon(name string, x, y, size int, color string) {
	entry, ok := iconRegistry[strings.ToLower(name)]
	if !ok {
		return
	}
	viewBox, err := parseViewBox(entry.ViewBox)
	if err != nil {
		return
	}
	scale := min(float64(size)/viewBox.Width, float64(size)/viewBox.Height)
	translateX := float64(x) + (float64(size)-viewBox.Width*scale)/2 - viewBox.MinX*scale
	translateY := float64(y) + (float64(size)-viewBox.Height*scale)/2 - viewBox.MinY*scale
	fmt.Fprintf(&s.Buffer, `<g transform="translate(%.4f %.4f) scale(%.6f)" fill="%s">`, translateX, translateY, scale, color)
	for _, path := range entry.Paths {
		fmt.Fprintf(&s.Buffer, `<path d="%s"/>`, path.D)
	}
	s.WriteString(`</g>`)
}

func (s *svgBuilder) iconPill(x, y int, name string) int {
	width := iconPillWidth(name)
	s.rect(x, y, width, 32, 8, readmeTheme.SurfaceAlt, readmeTheme.Border)
	s.icon(name, x+8, y+7, 18, readmeTheme.AccentSoft)
	s.text(x+34, y+21, "text small", name)
	return width
}

func iconPillWidth(value string) int {
	return runeLen(value)*7 + 46
}

func parseViewBox(value string) (iconViewBox, error) {
	parts := strings.Fields(strings.ReplaceAll(value, ",", " "))
	if len(parts) != 4 {
		return iconViewBox{}, fmt.Errorf("invalid viewBox %q", value)
	}
	values := make([]float64, 4)
	for index, part := range parts {
		parsed, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return iconViewBox{}, fmt.Errorf("invalid viewBox %q: %w", value, err)
		}
		values[index] = parsed
	}
	if values[2] <= 0 || values[3] <= 0 {
		return iconViewBox{}, fmt.Errorf("invalid viewBox dimensions %q", value)
	}
	return iconViewBox{
		MinX:   values[0],
		MinY:   values[1],
		Width:  values[2],
		Height: values[3],
	}, nil
}

func renderHeader(cfg config, profile githubProfile, fresh bool) []byte {
	s := newSVG(220, "Kristyan Carvalho", "Apresentação e links do perfil", "kristyancarvalho ~ %")
	s.text(38, 94, "text", "$ whoami")
	s.text(38, 126, "text label", "Kristyan Carvalho")
	s.text(38, 151, "text body muted", "Full-Stack Developer · São Paulo, Brasil")
	x := 38
	for _, item := range cfg.Links {
		x += s.pill(x, 171, item.Label, item.Label == "kristyan.dev") + 10
	}
	if fresh {
		s.text(730, 100, "text small muted", "github")
		s.text(730, 128, "text label accent", fmt.Sprintf("%d repos", profile.PublicRepos))
		s.text(730, 154, "text label soft", fmt.Sprintf("%d seguidores", profile.Followers))
	}
	return s.finish()
}

func renderAbout(cfg config) []byte {
	s := newSVG(265, "Sobre mim", "Resumo profissional e preferências de trabalho", "~/sobre-mim")
	for index, line := range cfg.Bio {
		class := "text body"
		if index == 0 {
			class = "text label"
		}
		s.text(38, 92+index*27, class, line)
	}
	s.line(600, 76, 600, 230, readmeTheme.Border, 1)
	s.text(630, 94, "text label accent", "gosto de construir")
	for index, item := range cfg.Focus {
		s.text(630, 128+index*31, "text body", "› "+item)
	}
	x := 38
	for _, tech := range []string{"TypeScript", "Go", "React", "React Native", "Node.js"} {
		x += s.pill(x, 206, tech, tech == "Go") + 9
	}
	return s.finish()
}

func renderConfig(cfg config) []byte {
	s := newSVG(225, "Configuração", "Ambiente de desenvolvimento e ferramentas", "~/.config")
	for index, item := range cfg.Environment {
		column := index % 5
		row := index / 5
		x := 38 + column*177
		y := 76 + row*68
		s.rect(x, y, 165, 54, 9, readmeTheme.Surface, readmeTheme.Border)
		s.icon(item.Value, x+12, y+16, 22, readmeTheme.Accent)
		s.text(x+44, y+21, "text small accent", item.Key)
		s.text(x+44, y+42, "text small", item.Value)
	}
	return s.finish()
}

func renderStack(cfg config) []byte {
	s := newSVG(520, "Stack", "Tecnologias agrupadas por área", "~/stack")
	positions := [][2]int{{38, 78}, {38, 222}, {38, 366}, {500, 78}, {500, 254}, {500, 430}}
	for index, group := range cfg.Stack {
		if index >= len(positions) {
			break
		}
		x, y := positions[index][0], positions[index][1]
		s.text(x, y+15, "text label accent", group.Label)
		renderPills(s, x, y+31, 410, group.Items)
	}
	return s.finish()
}

func renderPills(s *svgBuilder, startX, startY, maxWidth int, items []string) {
	x, y := startX, startY
	for _, item := range items {
		width := iconPillWidth(item)
		if x > startX && x+width > startX+maxWidth {
			x = startX
			y += 42
		}
		x += s.iconPill(x, y, item) + 8
	}
}

func renderBlog(cfg config, posts []blogPost, fresh bool) []byte {
	s := newSVG(350, "Blog", "Publicações mais recentes do blog", "~/blog")
	if !fresh || len(posts) == 0 {
		s.text(38, 104, "text body muted", "Feed temporariamente indisponível.")
		s.text(38, 136, "text body", cfg.BlogURL)
		return s.finish()
	}
	for index, post := range posts {
		y := 76 + index*86
		s.rect(38, y, 884, 68, 10, readmeTheme.Surface, readmeTheme.Border)
		s.text(54, y+25, "text label", truncate(post.Title, 88))
		date := formatRSSDate(post.Published)
		description := stripHTML(post.Description)
		if date != "" {
			description = date + " · " + description
		}
		description = truncate(description, 98)
		s.text(54, y+50, "text small muted", description)
	}
	s.text(38, 328, "text small accent", "blog.kristyan.dev →")
	return s.finish()
}

func renderGitHubStats(cfg config, data githubData, fresh bool) []byte {
	s := newSVG(445, "Estatísticas do GitHub", "Resumo dos repositórios públicos", "~/github")
	if !fresh {
		s.text(38, 104, "text body muted", "Dados do GitHub temporariamente indisponíveis.")
		s.text(38, 136, "text body", "github.com/"+cfg.GitHubUsername)
		return s.finish()
	}

	stars, forks := 0, 0
	languages := map[string]int{}
	for _, repo := range data.Repos {
		stars += repo.Stars
		forks += repo.Forks
		if !repo.Fork && !repo.Archived && repo.Language != "" {
			languages[repo.Language]++
		}
	}
	metric(s, 38, 76, "repositórios", strconv.Itoa(data.Profile.PublicRepos))
	metric(s, 252, 76, "estrelas", strconv.Itoa(stars))
	metric(s, 466, 76, "forks", strconv.Itoa(forks))
	metric(s, 680, 76, "seguidores", strconv.Itoa(data.Profile.Followers))

	s.text(38, 166, "text label accent", "linguagens principais")
	renderLanguageBars(s, languages)

	s.text(500, 166, "text label accent", "atualizados recentemente")
	recent := recentRepositories(data.Repos, cfg.GitHubUsername, cfg.RecentRepositoryLimit)
	for index, repo := range recent {
		y := 194 + index*54
		s.text(500, y, "text label", truncate(repo.Name, 26))
		detail := repo.Language
		if detail == "" {
			detail = "sem linguagem detectada"
		}
		s.text(500, y+22, "text small muted", fmt.Sprintf("%s · ★ %d · %s", detail, repo.Stars, relativeDate(repo.UpdatedAt)))
	}

	s.line(38, 372, 922, 372, readmeTheme.Border, 1)
	s.text(38, 405, "text label accent", "projetos em destaque")
	x := 240
	byName := make(map[string]repository, len(data.Repos))
	for _, repo := range data.Repos {
		byName[strings.ToLower(repo.Name)] = repo
	}
	for _, name := range cfg.HighlightProjects {
		label := name
		if repo, ok := byName[strings.ToLower(name)]; ok && repo.Language != "" {
			label += " · " + repo.Language
		}
		x += s.pill(x, 385, label, true) + 10
	}
	return s.finish()
}

func metric(s *svgBuilder, x, y int, label, value string) {
	s.rect(x, y, 190, 64, 10, readmeTheme.Surface, readmeTheme.Border)
	s.text(x+15, y+24, "text small muted", label)
	s.text(x+15, y+50, "text label accent", value)
}

func renderLanguageBars(s *svgBuilder, languages map[string]int) {
	type languageCount struct {
		Name  string
		Count int
	}
	counts := make([]languageCount, 0, len(languages))
	total := 0
	for name, count := range languages {
		counts = append(counts, languageCount{Name: name, Count: count})
		total += count
	}
	sort.Slice(counts, func(i, j int) bool {
		if counts[i].Count == counts[j].Count {
			return counts[i].Name < counts[j].Name
		}
		return counts[i].Count > counts[j].Count
	})
	if len(counts) > 5 {
		counts = counts[:5]
	}
	for index, item := range counts {
		y := 192 + index*34
		percent := 0
		if total > 0 {
			percent = item.Count * 100 / total
		}
		s.text(38, y+13, "text small", truncate(item.Name, 14))
		s.rect(150, y, 260, 14, 7, readmeTheme.SurfaceAlt, readmeTheme.Border)
		width := percent * 260 / 100
		if width < 8 {
			width = 8
		}
		s.rect(150, y, width, 14, 7, readmeTheme.AccentStrong, readmeTheme.AccentStrong)
		s.text(420, y+13, "text small muted", fmt.Sprintf("%d%%", percent))
	}
}

func recentRepositories(repos []repository, profileRepo string, limit int) []repository {
	filtered := make([]repository, 0, len(repos))
	for _, repo := range repos {
		if repo.Fork || repo.Archived || strings.EqualFold(repo.Name, profileRepo) {
			continue
		}
		filtered = append(filtered, repo)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt)
	})
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

func renderActivity(cfg config, events []githubEvent, fresh bool) []byte {
	s := newSVG(390, "Atividade no GitHub", "Eventos públicos recentes no GitHub", "~/atividade")
	if !fresh || len(events) == 0 {
		s.text(38, 104, "text body muted", "Atividade pública temporariamente indisponível.")
		return s.finish()
	}

	anchor := startOfDay(time.Now().UTC())
	counts := make(map[string]int)
	for _, event := range events {
		counts[event.CreatedAt.UTC().Format("2006-01-02")]++
	}
	maxCount := 1
	for _, count := range counts {
		if count > maxCount {
			maxCount = count
		}
	}

	s.text(38, 86, "text small muted", "eventos públicos · últimos 14 dias")
	for day := 13; day >= 0; day-- {
		date := anchor.AddDate(0, 0, -day)
		count := counts[date.Format("2006-01-02")]
		height := 8
		if count > 0 {
			height = 12 + count*70/maxCount
		}
		x := 38 + (13-day)*31
		s.rect(x, 185-height, 20, height, 5, activityColor(count, maxCount), activityColor(count, maxCount))
		if day%3 == 1 {
			s.text(x-2, 205, "text small muted", date.Format("02"))
		}
	}

	s.line(490, 76, 490, 350, readmeTheme.Border, 1)
	s.text(520, 86, "text label accent", "atividade recente")
	activities := meaningfulActivities(events, cfg.ActivityLimit)
	for index, activity := range activities {
		y := 122 + index*45
		s.text(520, y, "text body", truncate(activity.Description, 46))
		s.text(520, y+20, "text small muted", activity.When)
	}
	s.text(38, 344, "text small muted", "A API pública registra eventos recentes; contribuições privadas não são incluídas.")
	return s.finish()
}

func renderContributionStats(stats contributionStats) []byte {
	s := newSVG(465, "Contribuições agregadas", "Resumo anônimo de contribuições no GitHub", "~/contribuicoes")
	if !stats.Ready {
		s.text(38, 104, "text body muted", "Estatísticas agregadas temporariamente indisponíveis.")
		s.text(38, 136, "text body", stats.Note)
		s.text(38, 168, "text small muted", "O widget permanece seguro sem renderizar metadados privados.")
		s.pill(38, 198, "PROFILE_STATS_TOKEN", true)
		return s.finish()
	}

	metric(s, 38, 76, "commits", compactNumber(stats.TotalCommitContributions))
	metric(s, 252, 76, "contribuições", compactNumber(stats.TotalContributions))
	metric(s, 466, 76, "prs + reviews", compactNumber(stats.TotalPullRequestContribs+stats.TotalPullRequestReviewContribs))
	metric(s, 680, 76, "privado", contributionPrivateLabel(stats.PrivateMode))

	additions, deletions, touched := "n/d", "n/d", "n/d"
	if stats.Lines.Available {
		additions = compactNumber(stats.Lines.Additions)
		deletions = compactNumber(stats.Lines.Deletions)
		touched = compactNumber(stats.Lines.Additions + stats.Lines.Deletions)
	}
	metric(s, 38, 158, "linhas +", additions)
	metric(s, 252, 158, "linhas -", deletions)
	metric(s, 466, 158, "toque est.", touched)
	metric(s, 680, 158, "issues", compactNumber(stats.TotalIssueContributions))

	s.text(38, 260, "text label accent", "commits por ano")
	renderCommitTrend(s, 38, 282, 884, 82, stats.Years)

	s.line(38, 396, 922, 396, readmeTheme.Border, 1)
	scope := fmt.Sprintf("escopo: %s · linhas: %s", contributionPrivateLabel(stats.PrivateMode), contributionLineLabel(stats.LineMode))
	if stats.Note != "" {
		scope += " · " + stats.Note
	}
	s.text(38, 425, "text small muted", scope)
	s.text(38, 448, "text small muted", "Dados anonimizados: sem nomes de repositórios, branches, caminhos ou mensagens.")
	return s.finish()
}

func renderCommitTrend(s *svgBuilder, x, y, width, height int, years []yearContributionStats) {
	if len(years) == 0 {
		s.text(x, y+40, "text body muted", "Sem contribuições no período consultado.")
		return
	}
	if len(years) > 12 {
		years = years[len(years)-12:]
	}
	maxCommits := 1
	for _, item := range years {
		if item.Commits > maxCommits {
			maxCommits = item.Commits
		}
	}
	gap := 12
	barWidth := min(52, (width-gap*(len(years)-1))/len(years))
	if barWidth < 8 {
		barWidth = 8
	}
	totalWidth := barWidth*len(years) + gap*(len(years)-1)
	startX := x + (width-totalWidth)/2
	baseY := y + height
	for index, item := range years {
		barHeight := 8
		if item.Commits > 0 {
			barHeight = 16 + item.Commits*(height-18)/maxCommits
		}
		barX := startX + index*(barWidth+gap)
		barY := baseY - barHeight
		color := activityColor(item.Commits, maxCommits)
		s.rect(barX, barY, barWidth, barHeight, 6, color, color)
		s.text(barX+max(0, (barWidth-16)/2), baseY+22, "text small muted", fmt.Sprintf("%02d", item.Year%100))
	}
}

func compactNumber(value int) string {
	absolute := value
	if absolute < 0 {
		absolute = -absolute
	}
	switch {
	case absolute >= 1000000:
		return strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%.1fM", float64(value)/1000000), "0"), ".")
	case absolute >= 1000:
		return strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%.1fk", float64(value)/1000), "0"), ".")
	default:
		return strconv.Itoa(value)
	}
}

func contributionPrivateLabel(mode string) string {
	switch mode {
	case "agregado":
		return "agregado"
	case "limitado":
		return "limitado"
	case "token ausente":
		return "ausente"
	case "somente publico":
		return "público"
	default:
		return mode
	}
}

func contributionLineLabel(mode string) string {
	switch mode {
	case "prs acessiveis":
		return "PRs acessíveis"
	case "parcial":
		return "PRs parciais"
	case "indisponivel":
		return "n/d"
	default:
		return mode
	}
}

type activityItem struct {
	Description string
	When        string
}

func meaningfulActivities(events []githubEvent, limit int) []activityItem {
	result := make([]activityItem, 0, limit)
	seen := map[string]bool{}
	for _, event := range events {
		var payload eventPayload
		_ = json.Unmarshal(event.Payload, &payload)
		description := eventDescription(event, payload)
		key := event.Type + "|" + event.Repo.Name + "|" + payload.Action
		if description == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, activityItem{
			Description: description,
			When:        event.CreatedAt.UTC().Format("02 Jan 2006"),
		})
		if len(result) == limit {
			break
		}
	}
	return result
}

func eventDescription(event githubEvent, payload eventPayload) string {
	repo := strings.TrimPrefix(event.Repo.Name, "kristyancarvalho/")
	switch event.Type {
	case "PushEvent":
		if payload.Size > 0 {
			return fmt.Sprintf("push de %d commit(s) em %s", payload.Size, repo)
		}
		return "push em " + repo
	case "PullRequestEvent":
		action := translateAction(payload.Action)
		return fmt.Sprintf("pull request %s em %s", action, repo)
	case "IssuesEvent":
		action := translateAction(payload.Action)
		return fmt.Sprintf("issue %s em %s", action, repo)
	case "CreateEvent":
		if payload.RefType != "" {
			return fmt.Sprintf("%s criado em %s", payload.RefType, repo)
		}
		return "criação em " + repo
	case "ReleaseEvent":
		return "release publicada em " + repo
	case "WatchEvent":
		return "marcou " + event.Repo.Name + " com estrela"
	default:
		return ""
	}
}

func translateAction(action string) string {
	switch action {
	case "opened":
		return "aberta"
	case "closed":
		return "fechada"
	case "merged":
		return "integrada"
	case "created":
		return "criada"
	case "published":
		return "publicada"
	default:
		return action
	}
}

func activityColor(count, max int) string {
	if count == 0 {
		return readmeTheme.SurfaceAlt
	}
	ratio := count * 4 / max
	switch ratio {
	case 0, 1:
		return readmeTheme.ActivityLow
	case 2:
		return readmeTheme.ActivityMid
	case 3:
		return readmeTheme.AccentStrong
	default:
		return readmeTheme.Accent
	}
}

func updateReadme(cfg config) error {
	current, err := os.ReadFile(readmePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read README: %w", err)
	}
	block := readmeAssetBlock(cfg)
	content := string(current)
	if strings.Contains(content, startMarker) && strings.Contains(content, endMarker) {
		start := strings.Index(content, startMarker)
		end := strings.Index(content, endMarker)
		if end < start {
			return errors.New("README asset markers are out of order")
		}
		end += len(endMarker)
		content = content[:start] + block + content[end:]
	} else {
		content = block
	}
	return writeFileIfChanged(readmePath, []byte(strings.TrimSpace(content)+"\n"), 0o644)
}

func readmeAssetBlock(cfg config) string {
	return fmt.Sprintf(`%s
<div align="center">

<a href="%s"><img src="./assets/readme/header.svg" width="100%%" alt="Kristyan Carvalho — Full-Stack Developer" /></a>

<img src="./assets/readme/about.svg" width="100%%" alt="Sobre mim" />

<img src="./assets/readme/config.svg" width="100%%" alt="Configuração do ambiente de desenvolvimento" />

<img src="./assets/readme/stack.svg" width="100%%" alt="Stack de tecnologias" />

<a href="%s"><img src="./assets/readme/blog.svg" width="100%%" alt="Últimos posts do blog" /></a>

<a href="https://github.com/%s?tab=repositories"><img src="./assets/readme/github-stats.svg" width="100%%" alt="Estatísticas dos repositórios públicos no GitHub" /></a>

<a href="https://github.com/%s?tab=overview"><img src="./assets/readme/activity.svg" width="100%%" alt="Atividade pública recente no GitHub" /></a>

<img src="./assets/readme/contributions.svg" width="100%%" alt="Atividade agregada de contribuições no GitHub" />

</div>
%s`, startMarker, cfg.Links[0].URL, cfg.BlogURL, cfg.GitHubUsername, cfg.GitHubUsername, endMarker)
}

func writeSVG(path string, data []byte) error {
	if len(data) < 200 {
		return fmt.Errorf("refusing to write suspiciously small SVG %s", path)
	}
	var root struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("validate %s: %w", path, err)
	}
	if root.XMLName.Local != "svg" {
		return fmt.Errorf("validate %s: root element is %q", path, root.XMLName.Local)
	}
	return writeFileIfChanged(path, data, 0o644)
}

func writeFileIfChanged(path string, data []byte, mode os.FileMode) error {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, data) {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".readmegen-*")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("write temporary file for %s: %w", path, err)
	}
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return fmt.Errorf("chmod temporary file for %s: %w", path, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary file for %s: %w", path, err)
	}
	if err := os.Rename(tempName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	log.Printf("Updated %s", path)
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func esc(value string) string {
	return html.EscapeString(value)
}

func runeLen(value string) int {
	return utf8.RuneCountInString(value)
}

func truncate(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}

func stripHTML(value string) string {
	var output strings.Builder
	inTag := false
	for _, char := range value {
		switch char {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				output.WriteRune(char)
			}
		}
	}
	return html.UnescapeString(output.String())
}

func formatRSSDate(value string) string {
	parsed, err := time.Parse(time.RFC1123Z, value)
	if err != nil {
		parsed, err = time.Parse(time.RFC1123, value)
	}
	if err != nil {
		return ""
	}
	months := []string{"jan", "fev", "mar", "abr", "mai", "jun", "jul", "ago", "set", "out", "nov", "dez"}
	return fmt.Sprintf("%02d %s %d", parsed.Day(), months[parsed.Month()-1], parsed.Year())
}

func relativeDate(value time.Time) string {
	days := int(time.Since(value).Hours() / 24)
	switch {
	case days < 1:
		return "hoje"
	case days == 1:
		return "ontem"
	case days < 30:
		return fmt.Sprintf("há %d dias", days)
	case days < 365:
		return fmt.Sprintf("há %d meses", days/30)
	default:
		return fmt.Sprintf("há %d anos", days/365)
	}
}

func startOfDay(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, value.Location())
}
