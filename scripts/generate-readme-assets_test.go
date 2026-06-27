package main

import (
	"os"
	"strings"
	"testing"
	"time"
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

func TestTruncatePreservesUTF8(t *testing.T) {
	got := truncate("aplicações rápidas e explícitas", 18)
	if !utf8.ValidString(got) {
		t.Fatalf("truncate returned invalid UTF-8: %q", got)
	}
	if got != "aplicações rápida…" {
		t.Fatalf("truncate = %q", got)
	}
}

func TestRecentRepositoriesFiltersAndSorts(t *testing.T) {
	now := time.Now()
	repos := []repository{
		{Name: "profile", UpdatedAt: now},
		{Name: "fork", Fork: true, UpdatedAt: now.Add(-time.Hour)},
		{Name: "archived", Archived: true, UpdatedAt: now.Add(-2 * time.Hour)},
		{Name: "older", UpdatedAt: now.Add(-4 * time.Hour)},
		{Name: "newer", UpdatedAt: now.Add(-3 * time.Hour)},
	}

	got := recentRepositories(repos, "profile", 2)
	if len(got) != 2 {
		t.Fatalf("recentRepositories returned %d repositories", len(got))
	}
	if got[0].Name != "newer" || got[1].Name != "older" {
		t.Fatalf("recentRepositories order = %q, %q", got[0].Name, got[1].Name)
	}
}

func TestRenderedSVGIsSelfContained(t *testing.T) {
	cfg := config{
		Bio:   []string{"Full-Stack Developer em São Paulo."},
		Focus: []string{"software rápido e explícito"},
	}
	rendered := string(renderAbout(cfg))
	for _, forbidden := range []string{"<script", "<image", "@import", "url("} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("rendered SVG contains forbidden dependency %q", forbidden)
		}
	}
	if !strings.HasPrefix(rendered, "<svg") || !strings.HasSuffix(rendered, "</svg>") {
		t.Fatal("rendered output is not a complete SVG document")
	}
}

func TestConfigAndStackRenderIcons(t *testing.T) {
	cfg := config{
		Environment: []configItem{{Key: "os", Value: "Arch Linux"}},
		Stack:       []stackGroup{{Label: "Linguagens", Items: []string{"TypeScript"}}},
	}

	for name, rendered := range map[string]string{
		"config": string(renderConfig(cfg)),
		"stack":  string(renderStack(cfg)),
	} {
		if !strings.Contains(rendered, "<path") {
			t.Fatalf("%s card does not contain an inline icon", name)
		}
		if strings.Contains(rendered, "<image") {
			t.Fatalf("%s card contains an external image element", name)
		}
	}
}

func TestConfiguredIconsHaveVendoredSources(t *testing.T) {
	cfg, err := loadConfig("../readme.config.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateConfiguredIcons(cfg, iconRegistry); err != nil {
		t.Fatal(err)
	}
	for key, icon := range iconRegistry {
		if icon.Source == "" || icon.License == "" {
			t.Fatalf("icon %q has no source or license", key)
		}
		if len(icon.Paths) == 0 {
			t.Fatalf("icon %q has no path data", key)
		}
	}
}

func TestThemeDoesNotContainLegacyPurple(t *testing.T) {
	rendered := string(renderHeader(config{}, githubProfile{}, false))
	for _, color := range []string{"#b600ff", "#9d4edd", "#4b1763", "#70208f", "#9327bb"} {
		if strings.Contains(strings.ToLower(rendered), color) {
			t.Fatalf("rendered SVG contains legacy color %s", color)
		}
	}
}

func TestReadmeAssetBlockUsesContentVersions(t *testing.T) {
	cfg := config{
		GitHubUsername: "kristyancarvalho",
		BlogURL:        "https://blog.kristyan.dev",
		Links:          []link{{URL: "https://kristyan.dev"}},
	}
	versions := testReadmeAssetVersions()

	rendered := readmeAssetBlock(cfg, versions)
	for name, version := range versions {
		want := "./assets/readme/" + name + "?v=" + version
		if !strings.Contains(rendered, want) {
			t.Fatalf("README block does not contain versioned asset URL %q", want)
		}
	}
}

func TestContributionStatsRenderAggregatesOnly(t *testing.T) {
	stats := contributionStats{
		Ready:                          true,
		PrivateMode:                    "agregado",
		LineMode:                       "prs acessiveis",
		TotalContributions:             1234,
		TotalCommitContributions:       987,
		TotalPullRequestContribs:       12,
		TotalPullRequestReviewContribs: 5,
		RepositoriesWithCommits:        8,
		Years: []yearContributionStats{
			{Year: 2024, Commits: 120, Contributions: 180},
			{Year: 2025, Commits: 240, Contributions: 300},
		},
		Lines: contributionLineStats{
			Available: true,
			Additions: 1500,
			Deletions: 300,
		},
	}

	rendered := string(renderContributionStats(stats))
	for _, expected := range []string{"commits por ano", "linhas +", "toque est.", "Dados anonimizados"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("contribution widget missing %q", expected)
		}
	}
	for _, forbidden := range []string{"private-repo", "refs/heads", "commit message", "/src/private.go", "pull/"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("contribution widget leaked private metadata marker %q", forbidden)
		}
	}
}

func TestContributionStatsFallbackMentionsTokenWithoutBreakingSVG(t *testing.T) {
	rendered := string(renderContributionStats(fallbackContributionStats("token ausente", "indisponivel")))
	if !strings.Contains(rendered, "PROFILE_STATS_TOKEN") {
		t.Fatal("fallback widget does not mention PROFILE_STATS_TOKEN")
	}
	if !strings.HasPrefix(rendered, "<svg") || !strings.HasSuffix(rendered, "</svg>") {
		t.Fatal("fallback contribution widget is not a complete SVG")
	}
}

func TestReadmeAssetBlockAddsContributionWidgetAfterActivity(t *testing.T) {
	block := readmeAssetBlock(config{
		GitHubUsername: "kristyancarvalho",
		BlogURL:        "https://blog.kristyan.dev",
		Links:          []link{{URL: "https://kristyan.dev"}},
	}, testReadmeAssetVersions())
	activityIndex := strings.Index(block, "activity.svg")
	contributionIndex := strings.Index(block, "contributions.svg")
	if activityIndex == -1 || contributionIndex == -1 {
		t.Fatalf("README block missing activity or contribution widget")
	}
	if contributionIndex < activityIndex {
		t.Fatalf("contribution widget must appear below activity widget")
	}
}

func testReadmeAssetVersions() map[string]string {
	return map[string]string{
		"header.svg":        "header123",
		"about.svg":         "about123",
		"config.svg":        "config123",
		"stack.svg":         "stack123",
		"blog.svg":          "blog123",
		"github-stats.svg":  "stats123",
		"activity.svg":      "activity123",
		"contributions.svg": "contributions123",
	}
}
