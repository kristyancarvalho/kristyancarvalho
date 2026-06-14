package main

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

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
		if !strings.Contains(rendered, "<path") && !strings.Contains(rendered, "icon-label") {
			t.Fatalf("%s card does not contain an inline icon", name)
		}
		if strings.Contains(rendered, "<image") {
			t.Fatalf("%s card contains an external image element", name)
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
