# Interfaces, Data Files, and File Structure

## Interfaces

All state, runtime, and driver layers are defined as interfaces for testability. The state layer consists of pure value types and pure functions, while the runtime layer can be swapped with fakes during testing via backend interfaces.

```go
// state/state.go — All domain state (plain data, no methods)
type State struct {
    Sessions    map[SessionID]Session
    Active      WindowID
    Subscribers map[ConnID]Subscriber
    Jobs        map[JobID]JobMeta
    NextJobID   JobID
    NextConnID  ConnID
    Now         time.Time
    ShutdownReq bool
    Aliases     map[string]string
}

type Session struct {
    ID        SessionID
    Project   string
    Command   string
    WindowID  WindowID
    PaneID    string
    CreatedAt time.Time
    Driver    DriverState   // sum type: concrete state per driver impl
}
```

```go
// state/driver_iface.go — Driver interface (value-type plugin)
type Driver interface {
    Name() string
    DisplayName() string
    NewState(now time.Time) DriverState
    Step(prev DriverState, ev DriverEvent) (DriverState, []Effect, View)
    View(s DriverState) View
    SpawnCommand(s DriverState, baseCommand string) string
    Persist(s DriverState) map[string]string
    Restore(bag map[string]string, now time.Time) DriverState
}

// DriverState — closed sum type marker for per-session state
type DriverState interface {
    driverStateMarker()
}

// DriverEvent — input to Driver.Step (closed sum type)
// DEvTick, DEvHook, DEvJobResult, DEvFileChanged
```

Driver is a **value-type plugin**: no goroutines, no I/O, no mutexes. Per-session state is embedded in `state.Session.Driver` as a `DriverState` value, and round-trips as arguments and return values of `Driver.Step`. Side effects are returned as `[]Effect` and executed by the runtime's Effect interpreter.

```go
// state/status.go — Status enum
type Status int
const (
    StatusRunning Status = iota
    StatusWaiting
    StatusIdle
    StatusStopped
    StatusPending
)
```

```go
// state/reduce.go — Pure state transition function
func Reduce(s State, ev Event) (State, []Effect)
```

`Reduce` is the sole entry point for all state transitions. Event / Effect are closed sum types (`isEvent()` / `isEffect()` markers).

```go
// runtime/runtime.go — Imperative shell
type Runtime struct {
    cfg     Config
    state   state.State        // solely owned by the event loop goroutine
    eventCh chan state.Event    // Event submission from external goroutines
    workers *worker.Pool
    conns   map[state.ConnID]*ipcConn
    // ...
}

func (r *Runtime) Run(ctx context.Context) error  // event loop
func (r *Runtime) Enqueue(ev state.Event)          // goroutine-safe
```

```go
// runtime/backends.go — Backend interfaces swappable for testing
type TmuxBackend interface {
    SpawnWindow(name, cmd, startDir string, env map[string]string) (windowID, paneID string, err error)
    KillWindow(windowID string) error
    RunChain(args ...string) error
    SelectPane(target string) error
    SetStatusLine(line string) error
    PaneAlive(target string) (bool, error)
    // ...
}

type PersistBackend interface {
    Save(sessions []SessionSnapshot) error
    Load() ([]SessionSnapshot, error)
}
```

```go
// runtime/worker/pool.go — typed worker pool
// runtime/worker/registry.go — JobKind-based runner registry

func RegisterRunner[In state.JobInput, Out any](kind string, runner func(In) (Out, error))
func Dispatch(pool *Pool, jobID state.JobID, input state.JobInput)

type Pool struct { /* fixed-size goroutine pool */ }
func Submit[In state.JobInput, Out any](p *Pool, jobID state.JobID, input In, runner func(In) (Out, error))
func (p *Pool) Results() <-chan state.Event  // EvJobResult
```

```go
// proto/envelope.go — typed IPC wire format
type Envelope struct {
    Type   string          `json:"type"`     // "cmd" | "resp" | "evt"
    ReqID  string          `json:"req_id,omitempty"`
    Cmd    string          `json:"cmd,omitempty"`
    Name   string          `json:"name,omitempty"`
    Status string          `json:"status,omitempty"`
    Data   json.RawMessage `json:"data,omitempty"`
    Error  *ErrorBody      `json:"error,omitempty"`
}

// Command / Response / ServerEvent are closed sum types
type Command interface { isCommand(); CommandName() string }
```

Driver-specific hook payloads are passed through typed IPC as `proto.CmdEvent{Event, Timestamp, SenderID, Payload}`. Each driver subcommand (e.g., `roost event <eventType>`) packs its own hook payload into `CmdEvent` and sends it. The runtime's IPC reader converts it into an `EvEvent` Event and feeds it into the event loop. `reduceEvent` performs a single lookup via `Sessions[ev.SessionID]` and calls `Driver.Step(driverState, DEvHook{...})`. Neither the state layer nor the runtime layer hardcodes any driver-specific key names.

`Driver.SpawnCommand` is called from `runtime.Bootstrap` during cold start restoration, assembling the command string using driver-specific resume methods. The Claude driver holds the `session_id` received via `Restore` in DriverState and delegates to `lib/claude/cli.ResumeCommand` to return `claude --resume <id>`. The Generic driver returns the base command as-is.

## Data Files

| Path | Format | Contents | Lifecycle |
|------|--------|----------|-----------|
| `~/.roost/config.toml` | TOML | User settings (see below) | Created by user. Falls back to default values if absent |
| `~/.roost/sessions.json` | JSON | Session static metadata and Driver's `driver_state` (opaque map including status) — the single persistence store | Written on `EffPersistSnapshot` (on Tick / Hook event / session lifecycle changes). Read only at daemon startup via `runtime.Bootstrap`. Contents of `driver_state` are opaque key/value pairs interpreted by the driver; runtime knows none of the key names |
| `~/.roost/events/{sessionID}.log` | Text | Agent hook event log | Appended via `EffEventLogAppend`. The runtime's EventLogBackend manages file handles with lazy-open |
| `~/.roost/roost.log` | slog | Application log | Created/appended at daemon startup |
| `~/.roost/roost.sock` | Unix socket | Inter-process communication | Created at daemon startup. Deleted on exit |

Base path can be changed via `Config.DataDir` (set to TempDir during tests).

All fields in `settings.toml` (default values in parentheses):

- `tmux`: `session_name` (`"roost"`), `prefix` (`"C-b"`), `pane_ratio_horizontal` (`75`), `pane_ratio_vertical` (`70`)
- `monitor`: `poll_interval_ms` (`1000`), `idle_threshold_sec` (`30`)
- `session`: `auto_name` (`true`), `default_command` (`"claude"`), `commands` (`["claude","gemini","codex"]`)
- `projects`: `project_roots` (`["~/dev","~/work"]`)

## File Structure

```
src/
├── main.go              daemon / TUI mode branching (subcommand delegation via lib.Dispatch)
├── state/               Pure domain layer (no I/O, no goroutine)
│   ├── state.go         State, Session, Subscriber, JobMeta — plain value types
│   ├── event.go         Event closed sum type (EvCmdCreateSession, EvTick, EvJobResult, ...)
│   ├── effect.go        Effect closed sum type (EffSpawnTmuxWindow, EffStartJob, EffBroadcast, ...)
│   ├── reduce.go        Reduce(State, Event) → (State, []Effect) — pure state transition function
│   ├── reduce_session.go  session lifecycle reducers
│   ├── reduce_hook.go   hook event → Driver.Step routing
│   ├── reduce_tick.go   EvTick → stepAllSessions → Driver.Step(DEvTick)
│   ├── reduce_job.go    EvJobResult → Driver.Step(DEvJobResult)
│   ├── reduce_conn.go   IPC connection lifecycle
│   ├── reduce_lifecycle.go  shutdown / detach
│   ├── driver_iface.go  Driver interface (Step, View, Persist, Restore, SpawnCommand)
│   │                    DriverState / DriverEvent / DriverStateBase marker
│   ├── status.go        Status enum (Running/Waiting/Idle/Stopped/Pending)
│   ├── view.go          View / Card / Tag — display value types for TUI
│   ├── clone.go         Copy-on-write helpers for State
│   └── driver/          Driver implementations — value-type plugins (no goroutines, no I/O)
│       ├── claude.go    claudeDriver — event-driven status + transcript job emit
│       ├── claude_event.go  DEvHook dispatch (state-change, session-start, ...)
│       ├── claude_tick.go   DEvTick: active gate + transcript parse job emit
│       ├── claude_view.go   View() — Card, LogTabs, InfoExtras, StatusLine
│       ├── claude_persist.go  Persist / Restore — opaque bag round-trip
│       ├── generic.go   genericDriver — polling-driven (capture-pane job emit + hash comparison)
│       ├── generic_view.go  View()
│       ├── jobs.go      Job input/output types (TranscriptParseInput, CapturePaneInput, ...)
│       ├── poll.go      capture-pane shared helper for drivers
│       ├── tags.go      CommandTag helper
│       └── register.go  init() registers with state.Register
├── runtime/             Imperative shell — event loop + Effect interpreter
│   ├── runtime.go       Runtime.Run() — single event loop (select)
│   ├── interpret.go     execute(Effect) — interpreter for all side effects
│   ├── ipc.go           IPC server (accept, readLoop, writeLoop)
│   ├── backends.go      TmuxBackend, PersistBackend, EventLogBackend, FSWatcher interface
│   ├── tmux_real.go     TmuxBackend concrete implementation
│   ├── persist.go       PersistBackend concrete implementation (sessions.json)
│   ├── eventlog.go      EventLogBackend concrete implementation
│   ├── fsnotify.go      FSWatcher concrete implementation
│   ├── proto_bridge.go  proto.Command → state.Event conversion
│   ├── bootstrap.go     Initial State construction for warm/cold restart
│   ├── filerelay.go     File relay
│   ├── testing.go       Test helper (fake backend)
│   └── worker/          Worker pool
│       ├── pool.go      Pool + Submit[In,Out] (typed job submission)
│       ├── registry.go  RegisterRunner[In,Out] + Dispatch (JobKind-based runner registry)
│       └── runners.go   built-in runners (TranscriptParse, HaikuSummary, GitBranch, CapturePane)
├── proto/               Typed IPC — Command / Response / ServerEvent sum types
│   ├── envelope.go      Envelope wire format ({type, req_id, cmd|name, data})
│   ├── command.go       Command closed sum type
│   ├── response.go      Response closed sum type
│   ├── event.go         ServerEvent closed sum type
│   ├── codec.go         NDJSON encode/decode
│   ├── client.go        proto.Client (for TUI / palette / hook bridge)
│   ├── client_helpers.go  typed helpers (CreateSession, StopSession, ...)
│   ├── convert.go       state.View → proto.SessionInfo conversion
│   ├── reqid.go         Request ID generation
│   └── errors.go        ErrCode enum
├── tools/
│   └── tools.go         Tool + Param + ToolContext + Registry + DefaultRegistry
├── lib/
│   ├── subcommand.go    Subcommand registry (Register, Dispatch)
│   ├── git/
│   │   └── git.go       Git branch detection (DetectBranch)
│   └── claude/
│       ├── command.go   Claude subcommand handler (registers "claude" in init)
│       ├── hook.go      Claude hook event parsing
│       ├── setup.go     Hook registration/removal in Claude settings.json
│       ├── transcript/  Claude JSONL transcript parsing + diff tracking
│       └── cli/         Claude CLI launch command assembly (ResumeCommand etc.)
├── config/
│   └── config.go        TOML configuration loading
├── tmux/
│   ├── interfaces.go    PaneOperator
│   ├── client.go        tmux command wrapper (concrete implementation)
│   └── pane.go          Pane operations
├── tui/
│   ├── model.go         Session list Model (UI state only. Directly imports state.Status)
│   ├── view.go          Session list rendering
│   ├── mouse.go         Mouse input handler
│   ├── keys.go          Keybinding definitions + keyboard input handler
│   ├── main_model.go    Main TUI Model
│   ├── main_view.go     Main TUI rendering
│   ├── palette.go       Command palette
│   └── log_model.go     Log TUI (dynamic session tabs)
└── logger/
    └── logger.go        slog initialization
```
