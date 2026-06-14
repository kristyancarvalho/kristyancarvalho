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
