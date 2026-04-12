# Agent Roost

Launch AI agent sessions in seconds, see at a glance which ones need your attention, and switch to them instantly — even with dozens running in parallel. A tmux-native TUI for managing agent sessions across all your projects.

## Features

- Leverages tmux session management directly, so agent UIs work as-is
- Groups sessions by project for display
- Main pane always has focus; toggle to TUI with `prefix Space`
- Preview sessions in the main pane by moving the cursor
- Session list runs as an unkillable server; auto-recovers on crash
- `remain-on-exit` preserves layout when a pane exits

## Layout

```
┌───────────────────┬────────┐
│                   │ TUI    │
│  Pane 0: Agent    │ ▼ atlas│
│  (always focused) │  #1 ● │
│                   │  #2 ◆ │
├───────────────────┤ ▼ forge│
│  Pane 1: Log      │  #1 ○ │
└───────────────────┴────────┘
```

## Requirements

- Go 1.26+
- tmux 3.2+

## Installation

```bash
make install
```

Installs to `~/.local/bin/roost`.

## Usage

```bash
roost
```

Creates a tmux session (or attaches to an existing one) and launches with a 3-pane layout.

### Prefix Key

Default: `Ctrl+b` (same as tmux). Configurable via config.

| Key | Action |
|------|-----------|
| `prefix Space` | Toggle main pane ↔ TUI |
| `prefix d` | Detach (tmux stays alive; re-run `roost` to resume) |
| `prefix q` | Quit all (tmux session is destroyed) |
| `prefix p` | Command palette (shows tools with completion) |

### Command Palette

Displayed as a popup with `prefix p`. Filter tools by typing, press Enter to execute.

```
> new█
▸ new-session       Create session
  create-project    Create project + start session
```

| Tool | Description |
|--------|------|
| `new-session` | Create a session (select project and command) |
| `create-project` | Create a project directory and start a session |
| `stop-session` | Stop a session |
| `detach` | Detach (session stays alive) |
| `shutdown` | Quit all (tmux session is destroyed) |

### TUI Key Bindings (when TUI is focused)

| Key | Action |
|------|-----------|
| `j`/`k` or `↑`/`↓` | Select session (previews in main pane) |
| `Enter` | Switch to selected session → return to main |
| `n` | Quick launch (default command) |
| `N` | Launch with command selection |
| `d` | Stop session (with confirmation) |
| `Tab` | Collapse/expand project |
| `1`-`5` | Toggle status filter |
| `0` | Reset filter |

### Session States

| Display | State |
|------|------|
| `●` green | Running (producing output) |
| `◆` yellow | Waiting (awaiting input) |
| `◇` yellow | Pending approval (awaiting tool execution permission) |
| `○` gray | Idle (no output for 30+ seconds) |
| `■` red | Stopped |

## Configuration

```toml
# ~/.roost/settings.toml

[tmux]
session_name = "roost"
prefix = "C-Space"              # Prefix key (default: C-b)
pane_ratio_horizontal = 75      # Main pane width % (default: 75)
pane_ratio_vertical = 70        # Main pane height % (default: 70)

[monitor]
poll_interval_ms = 1000
idle_threshold_sec = 30

[session]
auto_name = true
default_command = "claude"
commands = ["claude", "gemini", "codex"]

[session.aliases]
cc = "claude"

[projects]
project_roots = ["~/dev", "~/work"]
project_paths = ["~/dotfiles"]
```

Works with default values even without a config file.
