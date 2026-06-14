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
	configPath   = "readme.config.json"
	readmePath   = "README.md"
	assetDir     = "assets/readme"
	startMarker  = "<!-- README-ASSETS:START -->"
	endMarker    = "<!-- README-ASSETS:END -->"
	githubAPIURL = "https://api.github.com"
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
	Path     string
	Stroke   bool
	Monogram string
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

var iconRegistry = map[string]icon{
	"arch linux":   {Path: "M12 2 2.5 21h4.2L12 11.8 17.3 21h4.2L12 2zm0 5.2 2.1 4.1-2.1-1.5-2.1 1.5L12 7.2z"},
	"hyprland":     {Path: "M5 4h14v16H5V4zm2 3v10h10V7H7zm2 2h6v2H9V9zm0 4h4v2H9v-2z"},
	"kitty":        {Path: "M4 5h16v14H4V5zm2 2v10h12V7H6zm1 3 3 3-3 3 1.4 1.4 4.4-4.4-4.4-4.4L7 10zm6 5h4v-2h-4v2z"},
	"zsh":          {Path: "M4 6h16v12H4V6zm2 2v8h12V8H6zm1.2 2 2 2-2 2 1.2 1.2 3.2-3.2-3.2-3.2L7.2 10zM12 15h4v-1.8h-4V15z"},
	"tmux":         {Path: "M4 4h16v16H4V4zm2 2v5h5V6H6zm7 0v5h5V6h-5zm-7 7v5h5v-5H6zm7 0v5h5v-5h-5z"},
	"neovim":       {Path: "M4 5.5 8 3v13.5l-4 3V5.5zm5-.8L13 2l7 5v11l-4 4-7-12.2V4.7zm4 3.1 3 6V8.2l-3-2.1v1.7z"},
	"go":           {Monogram: "Go"},
	"node.js":      {Path: "M12 2 21 7v10l-9 5-9-5V7l9-5zm0 2.4L5.2 8.2v7.6l6.8 3.8 6.8-3.8V8.2L12 4.4z"},
	"docker":       {Path: "M4 8h3v3H4V8zm4 0h3v3H8V8zm4 0h3v3h-3V8zm-4-4h3v3H8V4zm4 0h3v3h-3V4zm4 4h3v3h-3V8zM3 12h17c-.4 4.6-3.4 7-8.7 7C6.7 19 4 16.7 3 12z"},
	"git":          {Path: "M7 3a2 2 0 0 1 1.7 3l3.6 3.6a2 2 0 0 1 2.7 2.7l2 2a2 2 0 1 1-1.4 1.4l-2-2a2 2 0 0 1-2.7-2.7L7.3 7.4A2 2 0 1 1 7 3zm0 1.4a.6.6 0 1 0 0 1.2.6.6 0 0 0 0-1.2zm6.7 6.3a.6.6 0 1 0 0 1.2.6.6 0 0 0 0-1.2zm3.4 4.1a.6.6 0 1 0 0 1.2.6.6 0 0 0 0-1.2z"},
	"typescript":   {Monogram: "TS"},
	"javascript":   {Monogram: "JS"},
	"lua":          {Monogram: "Lu"},
	"shell":        {Path: "M4 5h16v14H4V5zm2 2v10h12V7H6zm1 3 3 3-3 3 1.4 1.4 4.4-4.4-4.4-4.4L7 10zm6 5h4v-2h-4v2z"},
	"html5":        {Monogram: "H5"},
	"css3":         {Monogram: "C3"},
	"express":      {Monogram: "Ex"},
	"fastify":      {Monogram: "Fa"},
	"gin":          {Monogram: "Gi"},
	"websocket":    {Path: "M6 7a7 7 0 0 1 11.8 3.2l-1.7 1A5 5 0 0 0 7.4 9L10 9v2H4V5h2v2zm12 10a7 7 0 0 1-11.8-3.2l1.7-1A5 5 0 0 0 16.6 15H14v-2h6v6h-2v-2z"},
	"jest":         {Monogram: "Je"},
	"rspec":        {Path: "M12 2 21 9l-9 13L3 9l9-7zm0 3.1L6.2 9.5 12 17.9l5.8-8.4L12 5.1z"},
	"playwright":   {Path: "M4 5h16v14H4V5zm2 2v10h12V7H6zm2.2 5.1 2.1 2.1 4.2-4.2 1.3 1.3-5.5 5.5-3.4-3.4 1.3-1.3z"},
	"react":        {Path: "M12 10.3a1.7 1.7 0 1 1 0 3.4 1.7 1.7 0 0 1 0-3.4zm0-4.8c4.8 0 8.7 2.9 8.7 6.5s-3.9 6.5-8.7 6.5S3.3 15.6 3.3 12 7.2 5.5 12 5.5zm0 2c-3.8 0-6.7 2.2-6.7 4.5s2.9 4.5 6.7 4.5 6.7-2.2 6.7-4.5S15.8 7.5 12 7.5zm5.6-1.7c2.4 1.4 1.8 5.9-.6 10s-5.9 7.1-8.3 5.7-1.8-5.9.6-10 5.9-7.1 8.3-5.7zm-1 1.7c-1.3-.8-3.8 1.2-5.6 4.9s-2 6.7-1.3 7.5c1.3.8 3.8-1.2 5.6-4.9s2-6.7 1.3-7.5z"},
	"next.js":      {Monogram: "Nx"},
	"react native": {Monogram: "RN"},
	"expo":         {Monogram: "Xp"},
	"vite":         {Path: "M5 3h14l-7 18L5 3zm3 3 4 10 4-10H8z"},
	"tailwindcss":  {Path: "M12 6c-3 0-4.9 1.5-5.7 4.5 1.1-1.5 2.5-2.1 4-1.7 2 .5 2.8 2.7 5.7 2.7 3 0 4.9-1.5 5.7-4.5-1.1 1.5-2.5 2.1-4 1.7C15.7 8.2 14.9 6 12 6zm-5.7 6c-3 0-4.9 1.5-5.7 4.5 1.1-1.5 2.5-2.1 4-1.7 2 .5 2.8 2.7 5.7 2.7 3 0 4.9-1.5 5.7-4.5-1.1 1.5-2.5 2.1-4 1.7-2-.5-2.8-2.7-5.7-2.7z"},
	"electron":     {Monogram: "El"},
	"postgresql":   {Monogram: "Pg"},
	"mongodb":      {Path: "M12 2c4 4.2 5 8.1 3.4 11.7-1 2.2-2.2 3.6-3.4 4.6-1.2-1-2.4-2.4-3.4-4.6C7 10.1 8 6.2 12 2zm0 4.2c-1.8 2.7-2.2 5-1.1 7.1.3.6.7 1.2 1.1 1.7.4-.5.8-1.1 1.1-1.7 1.1-2.1.7-4.4-1.1-7.1zM11 18h2v4h-2v-4z"},
	"redis":        {Path: "M12 3 21 7l-9 4-9-4 9-4zm-7.8 7L12 13.5 19.8 10 21 12l-9 4-9-4 1.2-2zm0 5L12 18.5l7.8-3.5 1.2 2-9 4-9-4 1.2-2z"},
	"sqlite":       {Monogram: "Sq"},
	"prisma":       {Path: "M13 2 21 19l-13 3L3 17 13 2zm-.5 5.2-5.8 9.2 2.1 2.1 8.8-2-5.1-9.3z"},
	"firebase":     {Path: "M5 20 8 4l4 7 3-9 4 18-7 3-7-3zm4.1-5.9-.7 3.8 3.6 1.5 3.8-1.6-1.5-7.7-2 5.9-3.2-1.9z"},
	"nginx":        {Monogram: "Nx"},
	"figma":        {Path: "M8 2h4v8H8a4 4 0 0 1 0-8zm0 2a2 2 0 1 0 0 4h2V4H8zm4-2h4a4 4 0 0 1 0 8h-4V2zm2 2v4h2a2 2 0 1 0 0-4h-2zM8 10h4v8H8a4 4 0 0 1 0-8zm0 2a2 2 0 1 0 0 4h2v-4H8zm4-2h4a4 4 0 1 1-4 4v-4zm2 2v2a2 2 0 1 0 2-2h-2zM8 18h4v2a4 4 0 1 1-4-4h4v2H8a2 2 0 1 0 2 2H8v-2z"},
}

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

	githubReady := profileErr == nil && reposErr == nil
	documents := []svgDocument{
		{Name: "header.svg", Content: renderHeader(cfg, gh.Profile, profileErr == nil), Dynamic: true, Ready: profileErr == nil},
		{Name: "about.svg", Content: renderAbout(cfg), Ready: true},
		{Name: "config.svg", Content: renderConfig(cfg), Ready: true},
		{Name: "stack.svg", Content: renderStack(cfg), Ready: true},
		{Name: "blog.svg", Content: renderBlog(cfg, posts, blogErr == nil), Dynamic: true, Ready: blogErr == nil},
		{Name: "github-stats.svg", Content: renderGitHubStats(cfg, gh, githubReady), Dynamic: true, Ready: githubReady},
		{Name: "activity.svg", Content: renderActivity(cfg, events, eventsErr == nil), Dynamic: true, Ready: eventsErr == nil},
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
		.icon-label{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:8px;font-weight:700;text-anchor:middle}
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
		entry = icon{Monogram: iconMonogram(name)}
	}
	if entry.Monogram != "" {
		fmt.Fprintf(&s.Buffer, `<text x="%d" y="%d" class="icon-label" fill="%s">%s</text>`, x+size/2, y+size/2+3, color, esc(entry.Monogram))
		return
	}
	scale := float64(size) / 24
	if entry.Stroke {
		fmt.Fprintf(&s.Buffer, `<path d="%s" transform="translate(%d %d) scale(%.4f)" fill="none" stroke="%s" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/>`, entry.Path, x, y, scale, color)
		return
	}
	fmt.Fprintf(&s.Buffer, `<path d="%s" transform="translate(%d %d) scale(%.4f)" fill="%s" fill-rule="evenodd"/>`, entry.Path, x, y, scale, color)
}

func (s *svgBuilder) iconPill(x, y int, name string) int {
	width := iconPillWidth(name)
	s.rect(x, y, width, 32, 8, readmeTheme.SurfaceAlt, readmeTheme.Border)
	s.icon(name, x+9, y+8, 16, readmeTheme.AccentSoft)
	s.text(x+32, y+21, "text small", name)
	return width
}

func iconPillWidth(value string) int {
	return runeLen(value)*7 + 44
}

func iconMonogram(value string) string {
	words := strings.FieldsFunc(value, func(r rune) bool {
		return r == ' ' || r == '.' || r == '-' || r == '&'
	})
	if len(words) > 1 {
		return strings.ToUpper(string([]rune(words[0])[0]) + string([]rune(words[1])[0]))
	}
	runes := []rune(value)
	if len(runes) == 0 {
		return "?"
	}
	if len(runes) > 2 {
		runes = runes[:2]
	}
	return strings.ToUpper(string(runes))
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
