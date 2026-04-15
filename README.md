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

### Hook Setup

Register roost hooks so agent status (● ◆ ◇ ○ ■) updates in real time. Run once per agent type:

```bash
roost claude setup    # registers hooks in ~/.claude/settings.json
roost codex setup     # registers hooks in ~/.codex/
roost gemini setup    # registers hooks in ~/.gemini/settings.json
```

Hooks are idempotent — re-running adds only missing entries and never overwrites existing config.

Without hooks, roost still launches sessions but status detection degrades to polling (idle detection only).

### Key Bindings

**Prefix bindings** work regardless of which pane is focused (ペイン移動・detach・palette 起動)。  
**SESSIONS pane bindings** are active only when the SESSIONS pane is focused (セッション操作)。

#### Prefix Keys

Default prefix: `Ctrl+b` (same as tmux default). Configurable via `[tmux] prefix`.

| Key | Action |
|------|-----------|
| `prefix Space` | Toggle MAIN ↔ SESSIONS pane |
| `prefix Escape` | Preview project |
| `prefix z` | Zoom MAIN pane |
| `prefix d` | Detach (tmux stays alive; re-run `roost` to resume) |
| `prefix q` | Quit all (tmux session is destroyed) |
| `prefix p` | Command palette |
| `prefix C-p` | Push driver palette (overlay a new agent onto the current session) |

#### Command Palette (`prefix p`)

Displayed as a popup. Filter tools by typing, press Enter to execute.

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

#### SESSIONS Pane Bindings

| Key | Action |
|------|-----------|
| `j`/`k` or `↑`/`↓` | Select session (previews in MAIN pane) |
| `Enter` | Switch to selected session → return to MAIN |
| `n` | Quick launch (default command) |
| `N` | Launch with command selection |
| `d` | Stop session |
| `Tab` | Collapse/expand project |
| `1`-`5` | Toggle status filter (Running / Waiting / Idle / Stopped / Pending) |
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

# data_dir = ""                 # Override config/data directory (default: ~/.roost)

[log]
level = "info"                  # "debug" | "info" | "warn" | "error"

[tmux]
session_name = "roost"
prefix = "C-b"                  # Prefix key
pane_ratio_horizontal = 75      # Main pane width % (1-99)
pane_ratio_vertical = 70        # Main pane height % (1-99)

[monitor]
poll_interval_ms = 1000         # Background polling interval
fast_poll_interval_ms = 100     # Polling interval while TUI is active
idle_threshold_sec = 30         # Seconds of silence before "Idle" (○)

[session]
auto_name = true                # Auto-generate session names
default_command = "shell"       # Command run by `n` (quick launch)
commands = [                    # Commands available via `N`
  "claude",
  "codex",
  "gemini",
  "shell",
]
push_commands = [               # Commands available via push-driver palette
  "shell",
  "git diff",
  "git diff --staged",
]

[projects]
project_roots = ["~/projects"]       # Subdirs of each root become projects
project_paths = ["~/myproject"] # Explicit project paths

[driver]
# summarize_command = ""        # Shell command to summarize transcripts (default: disabled)

[drivers.claude]
show_thinking = false           # Show extended thinking blocks in MAIN pane

```

Works with default values even without a config file.
