package main

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	feedURL     = "https://blog.kristyan.dev/rss.xml"
	readmePath  = "README.md"
	startMarker = "<!-- BLOG-POST-LIST:START -->"
	endMarker   = "<!-- BLOG-POST-LIST:END -->"
	postLimit   = 3
)

type rss struct {
	Channel struct {
		Items []item `xml:"item"`
	} `xml:"channel"`
}

type item struct {
	Title string `xml:"title"`
	Link  string `xml:"link"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "update blog posts: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	readme, err := os.ReadFile(readmePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", readmePath, err)
	}

	items, err := fetchItems()
	if err != nil {
		return err
	}

	updated, err := updateReadme(string(readme), items)
	if err != nil {
		return err
	}
	if updated == string(readme) {
		return nil
	}

	info, err := os.Stat(readmePath)
	if err != nil {
		return fmt.Errorf("stat %s: %w", readmePath, err)
	}
	if err := os.WriteFile(readmePath, []byte(updated), info.Mode()); err != nil {
		return fmt.Errorf("write %s: %w", readmePath, err)
	}

	return nil
}

func fetchItems() ([]item, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	response, err := client.Get(feedURL)
	if err != nil {
		return nil, fmt.Errorf("fetch RSS feed: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("fetch RSS feed: unexpected HTTP status %s", response.Status)
	}

	var feed rss
	if err := xml.NewDecoder(response.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("parse RSS feed: %w", err)
	}
	if len(feed.Channel.Items) == 0 {
		return nil, fmt.Errorf("RSS feed has no items")
	}

	limit := postLimit
	if len(feed.Channel.Items) < limit {
		limit = len(feed.Channel.Items)
	}

	return feed.Channel.Items[:limit], nil
}

func updateReadme(readme string, items []item) (string, error) {
	start := strings.Index(readme, startMarker)
	if start == -1 {
		return "", fmt.Errorf("blog marker block does not exist in %s", readmePath)
	}
	contentStart := start + len(startMarker)

	endOffset := strings.Index(readme[contentStart:], endMarker)
	if endOffset == -1 {
		return "", fmt.Errorf("blog marker block does not exist in %s", readmePath)
	}
	end := contentStart + endOffset

	posts := make([]string, 0, len(items))
	for index, item := range items {
		title := strings.TrimSpace(item.Title)
		link := strings.TrimSpace(item.Link)
		if title == "" || link == "" {
			return "", fmt.Errorf("RSS item %d is missing a title or link", index+1)
		}

		posts = append(posts, fmt.Sprintf("- [%s](%s)", escapeMarkdownTitle(title), link))
	}

	newline := "\n"
	if strings.Contains(readme, "\r\n") {
		newline = "\r\n"
	}

	block := newline + strings.Join(posts, newline) + newline
	return readme[:contentStart] + block + readme[end:], nil
}

func escapeMarkdownTitle(title string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`[`, `\[`,
		`]`, `\]`,
	)
	return replacer.Replace(title)
}
