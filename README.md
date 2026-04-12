# Agent Roost

**"Command your agent fleet with zero friction."**

Agent Roost is your mission control for orchestrating multiple AI agents and maximizing developer creativity. Stop wasting time managing terminal tabs or polling for progress, and transform your workflow into a seamless commanding experience.

### Value & Experience

- **Deploy Agents with Minimal Steps**: Break free from "launch rituals" like directory hopping, environment activation, or long command strings. Just pick a project and hit a key. Send your agents into the field with the absolute minimum of friction.
- **Turn Wait Time into Free Time**: See at a glance whether an agent is working, waiting for your input, or pending tool approval. Visualize the status of your entire fleet across all projects without ever having to wander through multiple terminals.
- **Zero-Friction Context Switching**: Jump into any session the instant intervention is needed. With high-speed previews just by moving your cursor, you can oversee dozens of concurrent tasks without breaking your cognitive flow.
- **An Unshakeable Foundation for Agents**: Built on the rock-solid architecture of tmux, your agents' thoughts never stop even if you close the UI or lose your connection. Roost provides the most stable "ground" for autonomous agents to keep running until the job is done.

## Layout

```text
┌───────────────────┬────────┐
│                   │SESSIONS│
│  Pane 0: MAIN     │ ▼ projA│
│  (always focused) │  #1 ● │
│                   │  #2 ◆ │
├───────────────────┤ ▼ projB│
│  Pane 1: LOG      │  #1 ○ │
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
| `prefix Space` | Toggle MAIN ↔ SESSIONS pane |
| `prefix Escape`| Preview project |
| `prefix z` | Zoom MAIN pane |
| `prefix d` | Detach (tmux stays alive; re-run `roost` to resume) |
| `prefix q` | Quit all (tmux session is destroyed) |
| `prefix p` | Command palette (shows tools with completion) |

### Command Palette

Displayed as a popup with `prefix p`. Filter tools by typing, press Enter to execute.

```text
> new█
▸ new-session       Create session
  create-project    Create new project dir and start session
```

| Tool | Description |
|--------|------|
| `new-session` | Create session |
| `create-project` | Create new project dir and start session |
| `stop-session` | Stop session |
| `detach` | Detach (keep session) |
| `shutdown` | Shutdown (discard sessions) |

### TUI Key Bindings (when SESSIONS pane is focused)

| Key | Action |
|------|-----------|
| `j`/`k` or `↑`/`↓` | Select session (previews in MAIN pane) |
| `Enter` | Switch to selected session → return to MAIN |
| `n` | Quick launch (default command) |
| `N` | Launch with command selection |
| `d` | Stop session |
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
