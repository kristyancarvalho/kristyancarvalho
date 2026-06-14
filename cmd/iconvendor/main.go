package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	registryPath    = "assets/readme/icons.json"
	attributionPath = "assets/readme/ICONS.md"
)

type sourceSpec struct {
	Key     string
	Name    string
	URL     string
	Project string
	License string
}

type iconRegistryFile struct {
	Icons map[string]icon `json:"icons"`
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

var sources = []sourceSpec{
	simpleIcon("arch linux", "Arch Linux", "archlinux"),
	simpleIcon("hyprland", "Hyprland", "hyprland"),
	{
		Key:     "kitty",
		Name:    "Kitty (console fallback)",
		URL:     "https://raw.githubusercontent.com/Templarian/MaterialDesign-SVG/v7.4.47/svg/console.svg",
		Project: "Material Design Icons 7.4.47",
		License: "Apache-2.0",
	},
	simpleIcon("zsh", "Zsh", "zsh"),
	simpleIcon("tmux", "tmux", "tmux"),
	simpleIcon("neovim", "Neovim", "neovim"),
	simpleIcon("go", "Go", "go"),
	simpleIcon("node.js", "Node.js", "nodedotjs"),
	simpleIcon("docker", "Docker", "docker"),
	simpleIcon("git", "Git", "git"),
	simpleIcon("typescript", "TypeScript", "typescript"),
	simpleIcon("javascript", "JavaScript", "javascript"),
	simpleIcon("lua", "Lua", "lua"),
	simpleIcon("shell", "GNU Bash", "gnubash"),
	simpleIcon("html5", "HTML5", "html5"),
	devicon("css3", "CSS3", "css3/css3-plain.svg"),
	simpleIcon("express", "Express", "express"),
	simpleIcon("fastify", "Fastify", "fastify"),
	simpleIcon("gin", "Gin", "gin"),
	simpleIcon("websocket", "Socket.IO", "socketdotio"),
	simpleIcon("jest", "Jest", "jest"),
	devicon("rspec", "RSpec", "rspec/rspec-plain.svg"),
	devicon("playwright", "Playwright", "playwright/playwright-plain.svg"),
	simpleIcon("react", "React", "react"),
	simpleIcon("next.js", "Next.js", "nextdotjs"),
	devicon("react native", "React Native", "reactnative/reactnative-original.svg"),
	simpleIcon("expo", "Expo", "expo"),
	simpleIcon("vite", "Vite", "vite"),
	simpleIcon("tailwindcss", "Tailwind CSS", "tailwindcss"),
	simpleIcon("electron", "Electron", "electron"),
	simpleIcon("postgresql", "PostgreSQL", "postgresql"),
	simpleIcon("mongodb", "MongoDB", "mongodb"),
	simpleIcon("redis", "Redis", "redis"),
	simpleIcon("sqlite", "SQLite", "sqlite"),
	simpleIcon("prisma", "Prisma", "prisma"),
	simpleIcon("firebase", "Firebase", "firebase"),
	simpleIcon("nginx", "NGINX", "nginx"),
	simpleIcon("figma", "Figma", "figma"),
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("vendor README icons: %v", err)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	client := &http.Client{Timeout: 20 * time.Second}
	registry := iconRegistryFile{Icons: make(map[string]icon, len(sources))}
	for _, source := range sources {
		parsed, err := fetchIcon(ctx, client, source)
		if err != nil {
			return fmt.Errorf("%s: %w", source.Key, err)
		}
		registry.Icons[source.Key] = parsed
		log.Printf("Loaded %s from %s", source.Name, source.Project)
	}

	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return fmt.Errorf("encode registry: %w", err)
	}
	data = append(data, '\n')
	if err := writeFile(registryPath, data); err != nil {
		return err
	}
	if err := writeFile(attributionPath, []byte(attributionMarkdown())); err != nil {
		return err
	}
	return nil
}

func simpleIcon(key, name, slug string) sourceSpec {
	return sourceSpec{
		Key:     key,
		Name:    name,
		URL:     "https://raw.githubusercontent.com/simple-icons/simple-icons/16.23.0/icons/" + slug + ".svg",
		Project: "Simple Icons 16.23.0",
		License: "CC0-1.0; trademarks remain property of their owners",
	}
}

func devicon(key, name, path string) sourceSpec {
	return sourceSpec{
		Key:     key,
		Name:    name,
		URL:     "https://raw.githubusercontent.com/devicons/devicon/v2.17.0/icons/" + path,
		Project: "Devicon 2.17.0",
		License: "MIT",
	}
}

func fetchIcon(ctx context.Context, client *http.Client, source sourceSpec) (icon, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return icon{}, err
	}
	request.Header.Set("User-Agent", "kristyancarvalho-readme-icon-vendor")
	response, err := client.Do(request)
	if err != nil {
		return icon{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return icon{}, fmt.Errorf("fetch %s: %s", source.URL, response.Status)
	}
	parsed, err := parseSVG(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return icon{}, fmt.Errorf("parse %s: %w", source.URL, err)
	}
	parsed.Name = source.Name
	parsed.Source = source.URL
	parsed.License = source.License
	return parsed, nil
}

func parseSVG(reader io.Reader) (icon, error) {
	decoder := xml.NewDecoder(reader)
	var parsed icon
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return icon{}, err
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "svg":
			parsed.ViewBox = attribute(start.Attr, "viewBox")
		case "path":
			if transform := attribute(start.Attr, "transform"); transform != "" {
				return icon{}, fmt.Errorf("path transforms are not supported: %q", transform)
			}
			if d := strings.TrimSpace(attribute(start.Attr, "d")); d != "" {
				parsed.Paths = append(parsed.Paths, iconPath{D: d})
			}
		}
	}
	if parsed.ViewBox == "" {
		return icon{}, errors.New("SVG has no viewBox")
	}
	if len(parsed.Paths) == 0 {
		return icon{}, errors.New("SVG has no path data")
	}
	return parsed, nil
}

func attribute(attributes []xml.Attr, name string) string {
	for _, attribute := range attributes {
		if attribute.Name.Local == name {
			return attribute.Value
		}
	}
	return ""
}

func attributionMarkdown() string {
	var builder strings.Builder
	builder.WriteString("# README icon sources\n\n")
	builder.WriteString("The generated README uses normalized SVG path data vendored in `icons.json`.\n")
	builder.WriteString("Scheduled README generation reads only that local file and performs no icon downloads.\n\n")
	builder.WriteString("Regenerate the registry intentionally with:\n\n")
	builder.WriteString("```sh\n")
	builder.WriteString("go run ./cmd/iconvendor\n")
	builder.WriteString("```\n\n")
	builder.WriteString("## Sources and licenses\n\n")
	builder.WriteString("- [Simple Icons 16.23.0](https://github.com/simple-icons/simple-icons/tree/16.23.0): CC0-1.0. Brand trademarks and project-specific usage guidelines still apply.\n")
	builder.WriteString("- [Devicon 2.17.0](https://github.com/devicons/devicon/tree/v2.17.0): MIT.\n")
	builder.WriteString("- [Material Design Icons 7.4.47](https://github.com/Templarian/MaterialDesign-SVG/tree/v7.4.47): Apache-2.0.\n\n")
	builder.WriteString("## Icon mapping\n\n")
	builder.WriteString("| README key | Icon | Source |\n")
	builder.WriteString("| --- | --- | --- |\n")
	sorted := append([]sourceSpec(nil), sources...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })
	for _, source := range sorted {
		fmt.Fprintf(&builder, "| `%s` | %s | %s |\n", source.Key, source.Name, source.Project)
	}
	builder.WriteString("\nKitty has no icon in the selected Simple Icons or Devicon releases, so it uses the established Material Design Icons console glyph rather than a custom approximation.\n")
	builder.WriteString("WebSocket uses the Socket.IO brand icon, matching the stack's existing WebSocket tooling context.\n")
	return builder.String()
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create directory for %s: %w", path, err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".iconvendor-*")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("write temporary file for %s: %w", path, err)
	}
	if err := temp.Chmod(0o644); err != nil {
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
