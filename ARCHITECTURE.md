# Architecture

This document describes the internal architecture of roost for developers.

**roost** is a TUI tool that centrally manages multiple AI agent sessions on tmux.

## Detailed Documentation

- [Process Model, tmux Layout, Rendering Responsibilities](docs/process-model.md) — Daemon/TUI process structure, pane layout, rendering boundary between Driver and TUI
- [Inter-Process Communication and Tool System](docs/ipc.md) — IPC message format, command list, concurrency model (event loop + worker pool), Tool abstraction
- [State Monitoring](docs/state-monitoring.md) — State detection via Driver plugins, Claude/Generic driver, persistence/restoration
- [Interface and File Reference](docs/interfaces.md) — Go type definitions, data files, source tree

## Vision

When running AI agents concurrently across multiple projects, raw tmux operations make it cumbersome to track and switch between sessions, and there is no visibility into whether each agent is idle/running/waiting. roost solves this.

- An operation panel that centrally manages multiple AI agent sessions across projects
- A thin TUI focused on session lifecycle management, without venturing into agent orchestration itself
- Start and switch sessions with minimal operations

## Design Principles

- **Functional Core / Imperative Shell**: All state transitions are expressed as a pure function `state.Reduce(state, event) → (state', effects)`. I/O is emitted as `Effect` values externally, and a single event loop (runtime) interprets them. No goroutines, mutexes, or actors exist in the state layer
- **tmux Native**: Directly leverage tmux sessions/windows/panes. Do not reimplement agent PTY
- **High-Level Operations as Tools**: Abstract high-level operations with side effects (session creation, stopping, termination) as Tools (`tools` package). The same Tool can be executed from both the TUI and the command palette
- **No Business Logic in TUI**: TUI handles display and key input only. Logic is consolidated in `state.Reduce` + `runtime`
- **Typed messages**: All boundaries (IPC command/response/event, state Event/Effect, driver DriverEvent) are closed sum types. Do not use closures as messages
- **Driver as Value Type**: Drivers are stateless plugins with no per-session state. Per-session state is embedded as a `DriverState` value in `state.Session.Driver` and round-trips as arguments and return values of `Driver.Step`. No goroutines or I/O
- **Effects are auditable**: All side-effects are `grep`-able as `state.Effect` enum constructors. Drivers do not directly write files or spawn subprocesses — the runtime interpreter executes everything
- **Single event loop**: All daemon state is exclusively owned by a single goroutine (the runtime event loop). No mutexes needed (except inside the worker pool). Fixed goroutine count (~16) independent of session count
- **Worker pool for slow I/O**: Transcript parse, haiku summary, git branch detect, and capture-pane run off-loop in a fixed-size worker pool (4 goroutines). Results feed back to the event loop as `EvJobResult`
- **Single persistence store**: `sessions.json` is the single source of truth. The only tmux user option is `@roost_id` (window-to-session marker). No dual bookkeeping
- **No fallbacks**: Do not synthesize "if source A is unavailable, use B". Until the Driver's Step updates the state, the status does not change
- **Testable design**: state.Reduce is tested with pure function tests at 90%+ coverage. Runtime is E2E tested with fake backends injected via interfaces. No mocks needed
- **Isolation of Driver-Specific Concepts**: Concepts specific to `driver/` and `lib/` (type names, constants, interfaces, configuration) must not be exposed to `state/`, `runtime/`, `tui/`, `proto/`, `config/`. Driver-specific TabKind constants are defined in each driver package. Driver-specific config is decoded by the driver into its own type from the `[drivers.<name>]` TOML section. Only main.go imports driver/lib as wiring
- **Isolation of Connector-Specific Concepts**: The same principle as Driver applies to Connectors. Concepts specific to `connector/` and `lib/` must not be exposed to `state/`, `runtime/`, `tui/`, `proto/`. Do not branch on connector name in TUI (`if name == "github"` is forbidden). Connectors hold no I/O (delegated to runtime via Effects). Job input/output types and runners are defined and registered within the `connector/` package
- **Pluggable Registration Pattern**: Driver plugin registration uses three init-time registries: `RegisterTabRenderer[C]`, `RegisterRunner[In,Out]`, and `state.Register(Driver)`. Connector plugin registration uses `RegisterRunner[In,Out]` and `state.RegisterConnector(Connector)`. runtime/tui dispatches via registries without knowing specific drivers/connectors

## Terminology

| Term | Meaning | tmux Entity |
|------|---------|-------------|
| **Session** | A unit of work for an AI agent. Managed by sessionID as `state.Session` (static metadata + DriverState) | tmux **window** (Window 1+, single pane configuration) |
| **Control Session** | The tmux session that houses all of roost | tmux **session** (`roost`) |
| **Pane** | Control panes within Window 0 | tmux **pane** (`0.0`, `0.1`, `0.2`) |
| **Connector** | A per-daemon external service integration plugin. Fetches data from external services like GitHub/Linear/Jira and displays it in the TUI. While Drivers are per-session, Connectors have one instance per daemon | None (holds no tmux resources) |
| **Warm start** | Runtime startup while a tmux session is alive. Restores state from sessions.json + tmux `@roost_id` | Reuses existing tmux session/window/pane |
| **Cold start** | Runtime startup when the tmux session is gone (PC reboot / tmux server death). Recreates tmux session/window from `sessions.json` | Creates new tmux session/window |

Hereafter, "session" refers to a roost session. tmux sessions are explicitly noted as "tmux session."

Runtime startup is always either a Warm start or a Cold start; there is no separate first-launch branch (if sessions.json does not exist, it simply Cold starts with an empty session list).

## Layer Structure

```
state/         Pure domain layer — State, Event, Effect, Reduce (no I/O, no goroutine)
driver/        Driver implementations — value-type Driver plugins + per-session DriverState. No I/O
connector/     Connector implementations — value-type Connector plugins + per-daemon ConnectorState. No I/O
runtime/       Imperative shell — single event loop, Effect interpreter, backend abstraction
runtime/worker/ Worker pool — slow I/O job execution (haiku, transcript parse, git, capture-pane, github fetch)
proto/         Typed IPC — Command / Response / ServerEvent sum types + wire codec
tools/         Palette tools — Tool abstraction for TUI + DefaultRegistry
tui/           Presentation layer — Bubbletea UI state management, rendering, key input
tmux/          Infrastructure layer — tmux command execution wrapper
lib/           Utilities — external tool integration (lib/git/, lib/claude/, lib/github/)
config/        Configuration — TOML loading, DataDir injection
logger/        Logging — slog initialization, log file management
```

The daemon process and TUI process are separate processes that communicate via typed IPC (`proto` package) over a Unix socket.

Code dependency direction:
- `main` → `runtime`, `driver`, `connector`, `proto`, `tools`, `tmux`, `config`, `logger`
- `runtime` → `state` (calls Reduce), `proto` (wire encode/decode), `runtime/worker` (Pool + Dispatch)
- `runtime/worker` → `state` (JobID, JobInput, EvJobResult). Does not import driver/connector/lib
- `state` is self-contained — imports no external packages (pure functional core)
- `driver` → `state` (DriverStateBase embed, Effect/View types), `runtime/worker` (RegisterRunner), `lib/*` (implementation)
- `connector` → `state` (ConnectorStateBase embed, Effect types), `runtime/worker` (RegisterRunner), `lib/*` (implementation)
- `proto` → `state` (carries Status enum, View/ConnectorSection types on wire)
- `tools` → `proto` (Client calls)
- `tui` → `proto` (Client + SessionInfo + ConnectorInfo), `state` (Status/View/ConnectorSection/TabRenderer types), `tools` (ToolRegistry). Does not import driver/connector/lib
- `lib/claude/command.go` (hook bridge) → `proto` (sends CmdHook), `config`
- `lib/claude/transcript` → `state` (registers TabRenderer factory via RegisterTabRenderer)
- `lib/subcommand.go` provides a subcommand registry. Each lib package registers in `init()`, and `main` dispatches via `lib.Dispatch`
- `state.Session` holds static metadata and DriverState (dynamic state) in a single struct. Reduce routes by sessionID and passes to Driver.Step
- `state.State.Connectors` holds per-daemon ConnectorState. Reduce routes by connector name and passes to Connector.Step

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Palette implementation approach | tmux popup (separate process) | Crash isolation. As a Bubbletea submodel, panics would be shared within the TUI |
| Ctrl+C disabling | Consume KeyPressMsg | Prevents accidental termination of the resident process. Pane becomes inoperable until respawn |
| No optimistic updates | Do not modify UI state on IPC error | Auto-recovers on next poll. Avoids risk of state inconsistency |
| shutdown (`C-b q`) behavior | Only `EffKillSession`; sessions.json is preserved | To restore from sessions.json on next startup |
| Claude startup on Cold start | Assemble `claude --resume <id>` via `Driver.SpawnCommand` | Claude-specific `--resume` knowledge is confined to `lib/claude/cli` |
| swap-pane rollback | Not performed | tmux's `;` chaining is not atomic and mid-rollback is impossible. State consistency is maintained even on Effect failure |
| IPC timeout | Not set | When the event loop deadlocks, external restart is the only recovery method |
| Session and Driver responsibility separation | `state.Session` holds static metadata + `DriverState` in a single struct | Since they are updated simultaneously within Reduce, state inconsistency is structurally impossible |
| Identifying sessionID for hook events | Inject env var via `tmux new-window -e ROOST_SESSION_ID=<id>` | Env var is set at kernel exec level and is race-free. Details in [state-monitoring.md](docs/state-monitoring.md#hook-event-routing-and-race-free-identification) |
| Hook payload abstraction | Carry `CmdHook.Payload` as an opaque `map[string]any` | Adding driver-specific fields requires no changes to state / runtime / proto |
| Connector scope | Per-daemon (one instance each), no state persistence (TTL-based), initialization on first EvTick | External service information is tied to the entire user account. Embedding in Driver would cause duplicate fetching. Initializing within the reducer enables pure function test coverage |

## Side-Effect Naming Convention

Distinguish path computation from side effects by function name.

| Pattern | Side Effect | Example |
|---------|-------------|---------|
| `XxxPath()` | None (pure) | `LogDirPath`, `ConfigDirPath`, `LogPath` |
| `EnsureXxx()` | Directory creation | `EnsureLogDir`, `EnsureConfigDir` |
| `LoadFrom(path)` | File read only | `config.LoadFrom` |
| `Load()` | Directory creation + file read | `config.Load` (convenience wrapper) |

## Testing Strategy

Test files are placed in the same directory as the target file as `*_test.go`.

- **state.Reduce tests**: No mocks needed. Pure function tests that directly verify the return value `(state', effects)` of `Reduce(state, event)`. No goroutine / channel / timing dependencies
- **Driver.Step tests**: No mocks needed. Directly verify the return value `(next, effects, view)` of `Step(prev, driverEvent)`
- **runtime tests**: Inject fakes for backend interfaces. Set `noopTmux` / `noopPersist` etc. in `runtime.Config` for testing. Inject `t.TempDir()` into `Config.DataDir` to isolate file I/O
- **TUI tests**: Pass messages directly to Bubbletea's `Model.Update` and verify the returned Model state. No actual terminal required

## Dependencies

| Package | Version | Purpose |
|---------|---------|---------|
| `charm.land/bubbletea/v2` | v2.0.2 | TUI framework |
| `charm.land/lipgloss/v2` | v2.0.2 | Styling |
| `charm.land/bubbles/v2` | v2.1.0 | Key bindings |
| `github.com/BurntSushi/toml` | v1.6.0 | Configuration file |
| `github.com/fsnotify/fsnotify` | v1.9.0 | File watching |
| `golang.org/x/term` | v0.41.0 | Terminal size detection |
