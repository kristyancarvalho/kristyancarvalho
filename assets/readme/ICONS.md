# README icon sources

The generated README uses normalized SVG path data vendored in `icons.json`.
Scheduled README generation reads only that local file and performs no icon downloads.

Regenerate the registry intentionally with:

```sh
go run ./cmd/iconvendor
```

## Sources and licenses

- [Simple Icons 16.23.0](https://github.com/simple-icons/simple-icons/tree/16.23.0): CC0-1.0. Brand trademarks and project-specific usage guidelines still apply.
- [Devicon 2.17.0](https://github.com/devicons/devicon/tree/v2.17.0): MIT.
- [Material Design Icons 7.4.47](https://github.com/Templarian/MaterialDesign-SVG/tree/v7.4.47): Apache-2.0.

## Icon mapping

| README key | Icon | Source |
| --- | --- | --- |
| `arch linux` | Arch Linux | Simple Icons 16.23.0 |
| `c` | C | Devicon 2.17.0 |
| `c++` | C++ | Devicon 2.17.0 |
| `css3` | CSS3 | Devicon 2.17.0 |
| `docker` | Docker | Simple Icons 16.23.0 |
| `git` | Git | Simple Icons 16.23.0 |
| `go` | Go | Simple Icons 16.23.0 |
| `html5` | HTML5 | Simple Icons 16.23.0 |
| `hyprland` | Hyprland | Simple Icons 16.23.0 |
| `java` | OpenJDK | Simple Icons 16.23.0 |
| `javascript` | JavaScript | Simple Icons 16.23.0 |
| `kitty` | Kitty (console fallback) | Material Design Icons 7.4.47 |
| `lua` | Lua | Simple Icons 16.23.0 |
| `neovim` | Neovim | Simple Icons 16.23.0 |
| `python` | Python | Simple Icons 16.23.0 |
| `qt` | Qt | Simple Icons 16.23.0 |
| `rust` | Rust | Simple Icons 16.23.0 |
| `shell` | GNU Bash | Simple Icons 16.23.0 |
| `tmux` | tmux | Simple Icons 16.23.0 |
| `typescript` | TypeScript | Simple Icons 16.23.0 |
| `zsh` | Zsh | Simple Icons 16.23.0 |

Kitty has no icon in the selected Simple Icons or Devicon releases, so it uses the established Material Design Icons console glyph rather than a custom approximation.
WebSocket uses the Socket.IO brand icon, matching the stack's existing WebSocket tooling context.
