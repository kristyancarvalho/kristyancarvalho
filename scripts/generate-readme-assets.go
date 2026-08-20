package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	profileAsset     = "profile.svg"
	iconRegistryPath = "assets/readme/icons.json"
	startMarker      = "<!-- README-ASSETS:START -->"
	endMarker        = "<!-- README-ASSETS:END -->"
)

type config struct {
	GitHubUsername string   `json:"githubUsername"`
	BlogURL        string   `json:"blogURL"`
	RSSURL         string   `json:"rssURL"`
	Environment    []string `json:"environment"`
	BlogPostLimit  int      `json:"blogPostLimit"`
}
type blogFeed struct {
	Channel struct {
		Items []blogPost `xml:"item"`
	} `xml:"channel"`
}
type blogPost struct {
	Title     string `xml:"title"`
	Link      string `xml:"link"`
	Published string `xml:"pubDate"`
}
type languageUsage struct {
	Name  string `json:"name"`
	Color string `json:"color"`
	Size  int64  `json:"size"`
}
type languageStat struct {
	Name, Color string
	Bytes       int64
	Percent     int
}
type pinnedRepository struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	URL         string        `json:"url"`
	Stars       int           `json:"stargazerCount"`
	Language    languageUsage `json:"primaryLanguage"`
	IsPrivate   bool          `json:"isPrivate"`
}
type profileData struct {
	Languages []languageStat
	Pins      []pinnedRepository
	Posts     []blogPost
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
type iconViewBox struct{ MinX, MinY, Width, Height float64 }
type graphQLClient struct {
	http  *http.Client
	token string
}
type svgBuilder struct {
	bytes.Buffer
	width, height int
}

var iconRegistry map[string]icon
var githubGraphQLURL = "https://api.github.com/graphql"
var theme = struct{ Background, Surface, Border, Text, Muted, Accent, Track string }{"#0d1117", "#161b22", "#30363d", "#f0f6fc", "#8b949e", "#58a6ff", "#21262d"}

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
	if err = validateConfiguredIcons(cfg, iconRegistry); err != nil {
		return err
	}
	if err = os.MkdirAll(assetDir, 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	httpClient := &http.Client{Timeout: 25 * time.Second}
	client := &graphQLClient{http: httpClient, token: strings.TrimSpace(os.Getenv("GH_TOKEN"))}
	github, githubErr := client.profile(ctx, cfg.GitHubUsername)
	if githubErr != nil {
		log.Printf("GitHub profile data unavailable: %v", githubErr)
	} else {
		log.Printf("GitHub profile data loaded: %d languages, %d pinned repositories", len(github.Languages), len(github.Pins))
	}
	posts, blogErr := fetchBlog(ctx, httpClient, cfg.RSSURL, cfg.BlogPostLimit)
	if blogErr != nil {
		log.Printf("Blog feed unavailable: %v", blogErr)
	} else {
		log.Printf("Blog posts loaded: %d", len(posts))
	}
	path := filepath.Join(assetDir, profileAsset)
	if githubErr != nil || blogErr != nil {
		if fileExists(path) {
			log.Printf("Keeping previous %s because required remote data is unavailable", path)
		} else {
			fallback := profileData{Posts: posts}
			if githubErr == nil {
				fallback.Languages, fallback.Pins = github.Languages, github.Pins
			}
			if err = writeSVG(path, renderProfile(cfg, fallback, false)); err != nil {
				return err
			}
		}
	} else if err = writeSVG(path, renderProfile(cfg, profileData{github.Languages, github.Pins, posts}, true)); err != nil {
		return err
	}
	if err = removeObsoleteAssets(); err != nil {
		return err
	}
	return updateReadme()
}
func loadConfig(path string) (config, error) {
	var cfg config
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	if strings.TrimSpace(cfg.GitHubUsername) == "" || strings.TrimSpace(cfg.BlogURL) == "" || strings.TrimSpace(cfg.RSSURL) == "" {
		return cfg, errors.New("config requires githubUsername, blogURL, and rssURL")
	}
	if cfg.BlogPostLimit <= 0 {
		cfg.BlogPostLimit = 3
	}
	if len(cfg.Environment) == 0 {
		return cfg, errors.New("config requires environment icons")
	}
	return cfg, nil
}
func loadIconRegistry(path string) (map[string]icon, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read icon registry: %w", err)
	}
	var registry iconRegistryFile
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(&registry); err != nil {
		return nil, fmt.Errorf("parse icon registry: %w", err)
	}
	if len(registry.Icons) == 0 {
		return nil, errors.New("icon registry is empty")
	}
	for key, value := range registry.Icons {
		if _, err := parseViewBox(value.ViewBox); err != nil || len(value.Paths) == 0 {
			return nil, fmt.Errorf("invalid icon %q", key)
		}
	}
	return registry.Icons, nil
}
func validateConfiguredIcons(cfg config, registry map[string]icon) error {
	for _, name := range cfg.Environment {
		if _, ok := registry[iconKey(name)]; !ok {
			return fmt.Errorf("icon registry is missing %q", name)
		}
	}
	return nil
}

type repositoryNode struct {
	Name       string `json:"name"`
	IsFork     bool   `json:"isFork"`
	IsArchived bool   `json:"isArchived"`
	Languages  struct {
		Edges []languageEdge `json:"edges"`
	} `json:"languages"`
}
type languageEdge struct {
	Size int64         `json:"size"`
	Node languageUsage `json:"node"`
}
type profileResponse struct {
	User *struct {
		PinnedItems struct {
			Nodes []pinnedRepository `json:"nodes"`
		} `json:"pinnedItems"`
		Repositories struct {
			Nodes    []repositoryNode `json:"nodes"`
			PageInfo struct {
				HasNextPage bool   `json:"hasNextPage"`
				EndCursor   string `json:"endCursor"`
			} `json:"pageInfo"`
		} `json:"repositories"`
	} `json:"user"`
}

func (client *graphQLClient) profile(ctx context.Context, username string) (profileData, error) {
	if client.token == "" {
		return profileData{}, errors.New("GH_TOKEN is required for authenticated GitHub GraphQL requests")
	}
	const query = `query($login:String!,$cursor:String){user(login:$login){pinnedItems(first:6,types:[REPOSITORY]){nodes{... on Repository{name description url isPrivate stargazerCount primaryLanguage{name color}}} }repositories(first:100,after:$cursor,ownerAffiliations:OWNER,privacy:PUBLIC){nodes{name isFork isArchived languages(first:100){edges{size node{name color}}}} pageInfo{hasNextPage endCursor}}}}`
	var all []repositoryNode
	var pins []pinnedRepository
	var cursor any
	for {
		var response profileResponse
		if err := client.query(ctx, query, map[string]any{"login": username, "cursor": cursor}, &response); err != nil {
			return profileData{}, err
		}
		if response.User == nil {
			return profileData{}, errors.New("GitHub user not found")
		}
		if pins == nil {
			for _, repository := range response.User.PinnedItems.Nodes {
				if !repository.IsPrivate {
					pins = append(pins, repository)
				}
			}
		}
		all = append(all, response.User.Repositories.Nodes...)
		page := response.User.Repositories.PageInfo
		if !page.HasNextPage || page.EndCursor == "" {
			break
		}
		cursor = page.EndCursor
	}
	return profileData{Languages: aggregateLanguages(all, username, 5), Pins: pins}, nil
}
func aggregateLanguages(repos []repositoryNode, username string, limit int) []languageStat {
	byName := map[string]languageStat{}
	for _, repo := range repos {
		if repo.IsFork || repo.IsArchived || strings.EqualFold(repo.Name, username) {
			continue
		}
		for _, edge := range repo.Languages.Edges {
			if edge.Node.Name == "" || edge.Size <= 0 {
				continue
			}
			v := byName[edge.Node.Name]
			v.Name = edge.Node.Name
			if v.Color == "" {
				v.Color = edge.Node.Color
			}
			v.Bytes += edge.Size
			byName[v.Name] = v
		}
	}
	all := make([]languageStat, 0, len(byName))
	var total int64
	for _, v := range byName {
		total += v.Bytes
		all = append(all, v)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Bytes == all[j].Bytes {
			return all[i].Name < all[j].Name
		}
		return all[i].Bytes > all[j].Bytes
	})
	for i := range all {
		if total > 0 {
			all[i].Percent = int((all[i].Bytes*100 + total/2) / total)
		}
	}
	if len(all) > limit {
		all = all[:limit]
	}
	return all
}
func (client *graphQLClient) query(ctx context.Context, query string, variables map[string]any, target any) error {
	payload, err := json.Marshal(struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}{query, variables})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubGraphQLURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "kristyancarvalho-readme-generator")
	req.Header.Set("Authorization", "Bearer "+client.token)
	resp, err := client.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == 401 {
			return errors.New("GitHub GraphQL authentication failed")
		}
		return fmt.Errorf("GitHub GraphQL returned %s", resp.Status)
	}
	var envelope struct {
		Data   json.RawMessage   `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&envelope); err != nil {
		return fmt.Errorf("decode GraphQL response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("GitHub GraphQL returned %d error(s)", len(envelope.Errors))
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return errors.New("GitHub GraphQL returned no data")
	}
	return json.Unmarshal(envelope.Data, target)
}
func fetchBlog(ctx context.Context, client *http.Client, url string, limit int) ([]blogPost, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "kristyancarvalho-readme-generator")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("RSS returned %s", resp.Status)
	}
	var feed blogFeed
	if err = xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("decode RSS: %w", err)
	}
	posts := make([]blogPost, 0, limit)
	for _, post := range feed.Channel.Items {
		post.Title = strings.TrimSpace(post.Title)
		post.Link = strings.TrimSpace(post.Link)
		if post.Title == "" || post.Link == "" {
			continue
		}
		posts = append(posts, post)
		if len(posts) == limit {
			break
		}
	}
	if len(posts) == 0 {
		return nil, errors.New("RSS feed has no valid posts")
	}
	return posts, nil
}

func newSVG(height int, title, desc string) *svgBuilder {
	s := &svgBuilder{width: 960, height: height}
	fmt.Fprintf(&s.Buffer, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-labelledby="title desc"><title id="title">%s</title><desc id="desc">%s</desc><style>text{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}.mono{font-family:ui-monospace,SFMono-Regular,Menlo,monospace}.title{font-size:30px;font-weight:700;fill:%s}.handle{font-size:16px;fill:%s}.section{font-size:14px;font-weight:700;letter-spacing:1.6px;fill:%s}.body{font-size:18px;fill:%s}.small{font-size:14px;fill:%s}.repo-desc{font-size:12px;fill:%s}</style><rect width="100%%" height="100%%" rx="16" fill="%s"/><rect x=".5" y=".5" width="959" height="%d" rx="15.5" fill="none" stroke="%s"/>`, s.width, s.height, s.width, s.height, esc(title), esc(desc), theme.Text, theme.Muted, theme.Accent, theme.Text, theme.Muted, theme.Muted, theme.Background, s.height-1, theme.Border)
	return s
}
func (s *svgBuilder) finish() []byte { s.WriteString("</svg>"); return s.Bytes() }
func (s *svgBuilder) text(x, y int, class, value string) {
	fmt.Fprintf(&s.Buffer, `<text x="%d" y="%d" class="%s">%s</text>`, x, y, class, esc(value))
}
func (s *svgBuilder) centeredText(x, y int, class, value string) {
	fmt.Fprintf(&s.Buffer, `<text x="%d" y="%d" text-anchor="middle" class="%s">%s</text>`, x, y, class, esc(value))
}
func (s *svgBuilder) rect(x, y, w, h, r int, fill string) {
	fmt.Fprintf(&s.Buffer, `<rect x="%d" y="%d" width="%d" height="%d" rx="%d" fill="%s"/>`, x, y, w, h, r, fill)
}
func (s *svgBuilder) line(x1, y1, x2, y2 int, color string) {
	fmt.Fprintf(&s.Buffer, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="%s"/>`, x1, y1, x2, y2, color)
}
func (s *svgBuilder) icon(key string, x, y, size int, color string) bool {
	value, ok := iconRegistry[iconKey(key)]
	if !ok {
		return false
	}
	vb, err := parseViewBox(value.ViewBox)
	if err != nil {
		return false
	}
	scale := float64(size) / maxFloat(vb.Width, vb.Height)
	tx := float64(x) + (float64(size)-vb.Width*scale)/2 - vb.MinX*scale
	ty := float64(y) + (float64(size)-vb.Height*scale)/2 - vb.MinY*scale
	fmt.Fprintf(&s.Buffer, `<g fill="%s" transform="translate(%.3f %.3f) scale(%.6f)">`, color, tx, ty, scale)
	for _, path := range value.Paths {
		fmt.Fprintf(&s.Buffer, `<path d="%s"/>`, path.D)
	}
	s.WriteString("</g>")
	return true
}
func renderProfile(cfg config, data profileData, fresh bool) []byte {
	s := newSVG(560, "Kristyan Carvalho", "GitHub projects, languages, development environment and latest writing")
	s.text(40, 48, "title", "Kristyan Carvalho")
	s.text(42, 71, "handle", "@"+cfg.GitHubUsername)
	s.line(40, 94, 920, 94, theme.Border)
	if !fresh {
		s.text(40, 113, "small", "GitHub data is temporarily unavailable; retained content will be used on the next refresh.")
	}
	s.text(40, 126, "section", "LANGUAGES IN CURRENT PUBLIC WORK")
	renderLanguages(s, data.Languages)
	s.text(470, 126, "section", "PINNED REPOSITORIES")
	renderPins(s, data.Pins)
	s.line(40, 350, 920, 350, theme.Border)
	s.text(40, 382, "section", "DEVELOPMENT ENVIRONMENT")
	renderEnvironment(s, cfg.Environment)
	s.line(40, 448, 920, 448, theme.Border)
	s.text(40, 480, "section", "LATEST WRITING")
	renderPosts(s, data.Posts)
	return s.finish()
}
func renderLanguages(s *svgBuilder, languages []languageStat) {
	if len(languages) == 0 {
		s.text(40, 165, "body", "No public language data available.")
		return
	}
	for i, item := range languages {
		y := 151 + i*37
		color := item.Color
		if color == "" {
			color = theme.Accent
		}
		key := languageIconKey(item.Name)
		if !s.icon(key, 40, y-14, 22, color) {
			s.rect(44, y-9, 12, 12, 6, color)
		}
		s.text(72, y+2, "body", truncate(item.Name, 18))
		s.text(330, y+2, "small", fmt.Sprintf("%d%%", item.Percent))
		s.rect(72, y+12, 290, 5, 3, theme.Track)
		w := item.Percent * 290 / 100
		if w < 5 {
			w = 5
		}
		s.rect(72, y+12, w, 5, 3, color)
	}
}
func renderPins(s *svgBuilder, pins []pinnedRepository) {
	if len(pins) == 0 {
		s.text(470, 165, "body", "No pinned repositories available.")
		return
	}
	for i, repo := range pins {
		if i == 6 {
			break
		}
		col, row := i%2, i/2
		x := 470 + col*225
		y := 148 + row*62
		s.rect(x, y, 207, 51, 8, theme.Surface)
		color := repo.Language.Color
		if color == "" {
			color = theme.Accent
		}
		if repo.Language.Name != "" {
			if !s.icon(languageIconKey(repo.Language.Name), x+12, y+13, 14, color) {
				s.rect(x+14, y+15, 10, 10, 5, color)
			}
		}
		s.text(x+34, y+22, "mono body", truncate(repo.Name, 20))
		desc := strings.TrimSpace(repo.Description)
		if desc == "" {
			desc = repo.Language.Name
		}
		s.text(x+12, y+42, "repo-desc", truncate(desc, 26))
	}
}
func renderEnvironment(s *svgBuilder, items []string) {
	x := 72
	for _, item := range items {
		if x > 856 {
			break
		}
		s.icon(item, x, 397, 26, theme.Text)
		s.centeredText(x+13, 438, "small", truncate(item, 11))
		x += 112
	}
}
func renderPosts(s *svgBuilder, posts []blogPost) {
	for i, post := range posts {
		if i == 3 {
			break
		}
		y := 510 + i*20
		s.text(40, y, "body", truncate(post.Title, 73))
		s.text(770, y, "small", formatRSSDate(post.Published))
	}
}
func languageIconKey(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "html":
		return "html5"
	case "css":
		return "css3"
	case "qml", "qt":
		return "qt"
	case "shell", "unix shell":
		return "shell"
	case "c++":
		return "c++"
	}
	return strings.ToLower(strings.TrimSpace(name))
}
func iconKey(value string) string { return strings.ToLower(strings.TrimSpace(value)) }
func parseViewBox(value string) (iconViewBox, error) {
	parts := strings.Fields(value)
	if len(parts) != 4 {
		return iconViewBox{}, errors.New("viewBox requires four values")
	}
	values := [4]float64{}
	for i, part := range parts {
		n, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return iconViewBox{}, err
		}
		values[i] = n
	}
	if values[2] <= 0 || values[3] <= 0 {
		return iconViewBox{}, errors.New("viewBox dimensions must be positive")
	}
	return iconViewBox{values[0], values[1], values[2], values[3]}, nil
}
func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
func updateReadme() error {
	data, err := os.ReadFile(filepath.Join(assetDir, profileAsset))
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])[:12]
	block := fmt.Sprintf("%s\n<div align=\"center\">\n  <img src=\"./assets/readme/profile.svg?v=%s\" width=\"100%%\" alt=\"Kristyan Carvalho — GitHub projects, languages and latest writing\" />\n</div>\n%s", startMarker, hash, endMarker)
	current, err := os.ReadFile(readmePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	content := string(current)
	if a, b := strings.Index(content, startMarker), strings.Index(content, endMarker); a >= 0 && b >= a {
		content = content[:a] + block + content[b+len(endMarker):]
	} else {
		content = block
	}
	return writeFileIfChanged(readmePath, []byte(strings.TrimSpace(content)+"\n"), 0o644)
}
func writeSVG(path string, data []byte) error {
	if len(data) < 200 {
		return fmt.Errorf("refusing to write suspiciously small SVG %s", path)
	}
	var doc struct{}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("validate SVG %s: %w", path, err)
	}
	return writeFileIfChanged(path, data, 0o644)
}
func writeFileIfChanged(path string, data []byte, mode os.FileMode) error {
	old, err := os.ReadFile(path)
	if err == nil && bytes.Equal(old, data) {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".readme-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	log.Printf("Updated %s", path)
	return nil
}
func removeObsoleteAssets() error {
	for _, name := range []string{"header.svg", "about.svg", "config.svg", "stack.svg", "blog.svg", "github-stats.svg", "activity.svg", "contributions.svg"} {
		path := filepath.Join(assetDir, name)
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }
func esc(value string) string     { return html.EscapeString(value) }
func truncate(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	r := []rune(value)
	if limit == 1 {
		return "…"
	}
	return string(r[:limit-1]) + "…"
}
func formatRSSDate(value string) string {
	for _, layout := range []string{time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822, time.RFC850} {
		if parsed, err := time.Parse(layout, strings.TrimSpace(value)); err == nil {
			return parsed.Format("02 Jan 2006")
		}
	}
	return ""
}
