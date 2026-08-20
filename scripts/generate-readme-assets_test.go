package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMain(m *testing.M) {
	var err error
	iconRegistry, err = loadIconRegistry("../assets/readme/icons.json")
	if err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
func node(name string, fork, archived bool, edges ...languageEdge) repositoryNode {
	r := repositoryNode{Name: name, IsFork: fork, IsArchived: archived}
	r.Languages.Edges = edges
	return r
}
func edge(size int64, name string) languageEdge {
	return languageEdge{Size: size, Node: languageUsage{Name: name}}
}
func TestLanguageAggregationFiltersRanksAndLimits(t *testing.T) {
	stats := aggregateLanguages([]repositoryNode{
		node("one", false, false, edge(100, "Go"), edge(50, "Rust")),
		node("two", false, false, edge(100, "Go"), edge(200, "TypeScript")),
		node("kristyancarvalho", false, false, edge(999, "Python")),
		node("fork", true, false, edge(999, "Java")),
		node("archive", false, true, edge(999, "C")),
	}, "kristyancarvalho", 2)
	if len(stats) != 2 || stats[0].Name != "Go" || stats[0].Bytes != 200 || stats[1].Name != "TypeScript" || stats[1].Bytes != 200 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
	if stats[0].Percent != 44 || stats[1].Percent != 44 {
		t.Fatalf("percentages: %#v", stats)
	}
}
func TestLanguageAggregationUsesDeterministicTies(t *testing.T) {
	got := aggregateLanguages([]repositoryNode{node("a", false, false, edge(10, "Zulu")), node("b", false, false, edge(10, "Alpha"))}, "profile", 5)
	if got[0].Name != "Alpha" || got[1].Name != "Zulu" {
		t.Fatalf("tie order: %#v", got)
	}
}

func TestPinnedRepositoriesDecodeInGraphQLOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		fmt.Fprint(w, `{"data":{"user":{"pinnedItems":{"nodes":[{"name":"first","description":"","url":"https://example.test/first","stargazerCount":2,"primaryLanguage":null},{"name":"second","description":"A repository","url":"https://example.test/second","stargazerCount":3,"primaryLanguage":{"name":"Go","color":"#00add8"}}]},"repositories":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}}`)
	}))
	defer server.Close()
	previous := githubGraphQLURL
	githubGraphQLURL = server.URL
	defer func() { githubGraphQLURL = previous }()
	data, err := (&graphQLClient{http: server.Client(), token: "test"}).profile(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Pins) != 2 || data.Pins[0].Name != "first" || data.Pins[1].Name != "second" || data.Pins[1].Language.Name != "Go" {
		t.Fatalf("unexpected pins: %#v", data.Pins)
	}
}

func TestBlogFetchKeepsOnlyThreeValidPosts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<rss><channel><item><title>invalid</title></item><item><title>one</title><link>https://example.test/1</link></item><item><title>two</title><link>https://example.test/2</link></item><item><title>three</title><link>https://example.test/3</link></item><item><title>four</title><link>https://example.test/4</link></item></channel></rss>`)
	}))
	defer server.Close()
	posts, err := fetchBlog(context.Background(), server.Client(), server.URL, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 3 || posts[0].Title != "one" || posts[2].Title != "three" {
		t.Fatalf("unexpected posts: %#v", posts)
	}
}
func TestProfileRenderingFallbackAndSafety(t *testing.T) {
	cfg := config{GitHubUsername: "k", Environment: []string{"Git"}}
	got := string(renderProfile(cfg, profileData{Languages: []languageStat{{Name: "Unknown Language", Color: "#123456", Percent: 100}}, Pins: []pinnedRepository{{Name: "first"}, {Name: "second"}, {Name: "third", Language: languageUsage{Name: "Go"}}}, Posts: []blogPost{{Title: "one"}, {Title: "two"}, {Title: "three"}, {Title: "four"}}}, true))
	for _, bad := range []string{"<script", "<image", "@import", "url("} {
		if strings.Contains(got, bad) {
			t.Fatalf("contains %q", bad)
		}
	}
	if !strings.HasPrefix(got, "<svg") || !strings.HasSuffix(got, "</svg>") || !strings.Contains(got, "<title") || !strings.Contains(got, "viewBox") || !strings.Contains(got, "<path") {
		t.Fatal("missing required SVG content")
	}
	if strings.Contains(got, ">four<") {
		t.Fatal("rendered more than three posts")
	}
	if !(strings.Index(got, "first") < strings.Index(got, "second") && strings.Index(got, "second") < strings.Index(got, "third")) {
		t.Fatal("pinned order not preserved")
	}
	if err := xml.Unmarshal([]byte(got), new(struct{})); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}
}
func TestUnicodeTruncation(t *testing.T) {
	got := truncate("aplicações rápidas e explícitas", 18)
	if got != "aplicações rápida…" || !utf8.ValidString(got) {
		t.Fatalf("truncate = %q", got)
	}
}

func TestReadmeReferencesOnlyProfileAsset(t *testing.T) {
	data, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	readme := string(data)
	if strings.Count(readme, "<img") != 1 || !strings.Contains(readme, "profile.svg?v=") {
		t.Fatalf("README does not contain one cache-busted profile asset: %s", readme)
	}
	for _, obsolete := range []string{"header.svg", "about.svg", "config.svg", "stack.svg", "blog.svg", "github-stats.svg", "activity.svg", "contributions.svg"} {
		if strings.Contains(readme, obsolete) {
			t.Fatalf("README still references %s", obsolete)
		}
	}
	profile, err := os.ReadFile("../assets/readme/profile.svg")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(profile)
	hash := hex.EncodeToString(sum[:])[:12]
	if !strings.Contains(readme, "profile.svg?v="+hash) {
		t.Fatal("README cache-buster does not match the profile asset")
	}
}
