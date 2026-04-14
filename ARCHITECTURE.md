# Architecture

## Vision

When running AI agents across multiple projects, you lose track of which agents are working, which are waiting for input, and which need tool approval. Switching between them in raw tmux is slow and error-prone. roost solves this: launch sessions in seconds, see their status at a glance, and switch instantly.

roost is a session lifecycle manager — not an agent orchestrator. It does not control what agents do; it gives you visibility and fast access to all of them from a single tmux-based TUI.

## Design Principles

- **Functional Core / Imperative Shell**: All state transitions are expressed as a pure function `state.Reduce(state, event) → (state', effects)`. I/O is emitted as `Effect` values and interpreted by a single event loop (runtime). No goroutines, mutexes, or actors exist in the state layer
- **Driver as Value Type**: Drivers are stateless plugins. Per-session state is embedded as a `DriverState` value in `state.Session.Driver` and round-trips through `Driver.Step`. No goroutines, no I/O — side effects are returned as `[]Effect`
- **Single event loop**: All daemon state is exclusively owned by one goroutine. No mutexes needed (except inside the worker pool). Slow I/O (transcript parse, capture-pane, etc.) runs in a fixed-size worker pool and feeds results back as events
- **Driver/Connector isolation**: Concepts specific to `driver/` and `connector/` must not leak into `state/`, `runtime/`, `tui/`, or `proto/`. TUI never branches on driver or connector name. Only `main.go` imports driver/connector packages as wiring
- **No fallbacks**: Do not synthesize "if source A is unavailable, use B". Until `Driver.Step` updates the state, the status does not change

## Documentation

- [Process Model, tmux Layout, Rendering Responsibilities](docs/process-model.md) — Daemon/TUI process structure, pane layout, rendering boundary between Driver and TUI
- [Inter-Process Communication and Tool System](docs/ipc.md) — IPC message format, command list, concurrency model (event loop + worker pool), Tool abstraction
- [State Monitoring](docs/state-monitoring.md) — State detection via Driver plugins, Claude/Generic driver, persistence/restoration
- [Interface and File Reference](docs/interfaces.md) — Go type definitions, data files, source tree

## Terminology

| Term | Meaning | tmux Entity |
|------|---------|-------------|
| **Session** | A unit of work for an AI agent. Managed by sessionID as `state.Session` (static metadata + DriverState) | tmux **pane** (parked in a background window, swapped into `0.0` when active) |
| **Control Session** | The tmux session that houses all of roost | tmux **session** (`roost`) |
| **Pane** | Control panes within Window 0 | tmux **pane** (`0.0`, `0.1`, `0.2`) |
| **Connector** | A per-daemon external service integration plugin. Fetches data from external services like GitHub/Linear/Jira and displays it in the TUI. While Drivers are per-session, Connectors have one instance per daemon | None (holds no tmux resources) |
| **Warm start** | Runtime startup while a tmux session is alive. `LoadSnapshot()` → `LoadSessionPanes()` (reads `ROOST_SESSION_*` env vars via `tmux show-environment`) → `ReconcileOrphans()` (drops sessions without panes, cleans stale env vars) | Reuses existing tmux session/pane |
| **Cold start** | Runtime startup when the tmux session is gone (PC reboot / tmux server death). `LoadSnapshot()` → `RecreateAll()` (spawns panes, populates `sessionPanes` + `ROOST_SESSION_*` env vars) | Creates new tmux session/window |

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
features/      Feature flags — Flag/Set types (runtime), build-tag const (compile-time). No external deps
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
- `lib/claude/command.go` (hook bridge) → `event` (sends CmdEvent via event.Send), `config`
- `lib/claude/transcript` → `state` (registers TabRenderer factory via RegisterTabRenderer)
- `cli/subcommand.go` provides a subcommand registry. Each lib package registers in `init()`, and `main` dispatches via `cli.Dispatch`
- `event/send.go` (event subcommand) → `proto` (sends CmdEvent), `cli` (registers "event" subcommand)
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
| Resident tracking | `SessionID -> PaneID` | Pane identity survives `swap-pane`; no parked window index tracking is needed |
| IPC timeout | Not set | When the event loop deadlocks, external restart is the only recovery method |
| Session and Driver responsibility separation | `state.Session` holds static metadata + `DriverState` in a single struct | Since they are updated simultaneously within Reduce, state inconsistency is structurally impossible |
| Identifying sessionID for hook events | Inject env var via `tmux new-window -e ROOST_SESSION_ID=<id>` | Env var is set at kernel exec level and is race-free. Details in [state-monitoring.md](docs/state-monitoring.md#hook-event-routing-and-race-free-identification) |
| Hook payload abstraction | Carry `CmdEvent.Payload` as an opaque `json.RawMessage` | Adding driver-specific fields requires no changes to state / runtime / proto |
| Agent event integration | `roost event <eventType>` → `proto.CmdEvent` → `EvEvent` → `reduceEvent` → `Driver.Step(DEvHook)` | Hook bridge identifies sessionID race-free via `$ROOST_SESSION_ID` env var. Reducer performs a single lookup via `Sessions[ev.SessionID]`. Details in [state-monitoring.md](docs/state-monitoring.md#hook-event-routing-and-race-free-identification) |
| Connector scope | Per-daemon (one instance each), no state persistence (TTL-based), initialization on first EvTick | External service information is tied to the entire user account. Embedding in Driver would cause duplicate fetching. Initializing within the reducer enables pure function test coverage |

## Feature Flags

Experimental features are gated by one of **two independent mechanisms**. They share no key space — pick one based on whether the experimental code should physically exist in the binary.

| Mechanism | Where defined | Toggle | Code in binary? | Use when |
|---|---|---|---|---|
| **Runtime flag** | `features.Flag` constant + `features.Set` injected into `state.State` | `~/.roost/settings.toml` `[features.enabled]` | Yes (both branches always compiled) | The user should be able to opt-in without rebuilding |
| **Compile-time flag** | `features` package `const` guarded by `//go:build <tag>` | `go build -tags <tag>` (e.g. `make build-experimental`) | No (off-side is removed by dead code elimination) | The experimental code is unfinished, unsafe, or should not enter release binaries |

The C analogue: runtime flag is `if () {}`, compile-time flag is `#if / #endif`.

### Runtime flag — how to add

1. Add a `Flag` constant in `features/features.go` and append it to `features.All()`.
2. Reference it where needed: `if st.Features.On(features.MyFeature) { ... }`. Allowed in `state/`, `runtime/`, `tui/`. **Not** in `driver/` or `connector/` (driver-specific gating uses `config.Drivers[name]` instead).
3. Users opt in via:
   ```toml
   [features.enabled]
   my-feature = true
   ```
4. When the feature stabilises, delete the constant and inline the enabled branch. Unknown keys in user config are silently ignored, so no migration is needed.

### Compile-time flag — how to add

1. Create paired files in `features/` guarded by build tag:
   ```go
   //go:build my_feat
   package features
   const MyFeat = true
   ```
   ```go
   //go:build !my_feat
   package features
   const MyFeat = false
   ```
2. Gate code with `if features.MyFeat { ... }`. Because `MyFeat` is a `const`, the off-side branch is eliminated entirely from the binary.
3. For larger experimental code, put the implementation in a `//go:build my_feat` file and provide a no-op stub in `//go:build !my_feat`. Callers do not need to be guarded.
4. Add a Makefile target for first-class build variants (e.g. `make build-experimental`). CI should build both variants.

### What goes where

- The `features/` package imports nothing outside the standard library — `state/` can depend on it without breaking the self-contained core.
- `state.State.Features` is set once at startup and never mutated, preserving Reduce's purity.
- `tui/` receives the active flag list over `proto` (daemon → tui via `EvtSessionsChanged.Features`) and rebuilds its own `features.Set`. `proto` carries it as `[]string`, matching the existing pattern of crossing the wire as primitives.

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
