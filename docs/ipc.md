# Inter-Process Communication (IPC) and Tool System

## Inter-Process Communication (IPC)

JSON messaging over a Unix domain socket (`~/.roost/roost.sock`).

### Topology

```mermaid
flowchart TB
    subgraph daemon["Daemon process (runtime.Runtime)"]
        EL["Event loop<br/>select { eventCh | internalCh | ticker | workers.Results | watcher.Events }"]
        Reduce["state.Reduce(state, event)<br/>→ (state', []Effect)<br/>pure function: no goroutine, no I/O"]
        Interp["Effect interpreter<br/>runtime.execute(eff)<br/>tmux / IPC / persist / worker"]
        Pool["Worker pool (4 goroutine)<br/>worker.Dispatch<br/>JobKind-based runner registry"]

        EL --> Reduce
        Reduce --> Interp
        Interp -->|EffStartJob| Pool
        Pool -->|EvJobResult| EL
    end

    subgraph tui["Session list TUI"]
        Client["proto.Client<br/>Envelope send/receive / responses ch / events ch"]
        Model["Model<br/>UI state management / key input / Cmd dispatch"]
        View["View<br/>Rendering"]

        Client --> Model --> View
    end

    Client <-->|"Unix socket<br/>NDJSON (proto.Envelope)"| EL
```

`runtime.Runtime` is the sole state owner. `state.State` is a pure value type that only round-trips as an argument and return value of `Reduce`. The effect interpreter performs tmux operations, IPC sends, persistence, and worker pool submits, feeding results back to the event loop as `Event`s.

**Runtime composition**:
- `state`: `state.State` — all domain state (Sessions map, Active, Subscribers, Jobs). Solely owned by the event loop goroutine
- `eventCh`: channel where external goroutines (IPC reader, worker pool, fsnotify watcher) submit Events
- `workers`: `worker.Pool` — fixed-size (4) goroutine pool. `worker.Dispatch` dispatches via registered runner lookup using `JobInput.JobKind()`
- `conns`: `map[ConnID]*ipcConn` — connection management. Solely owned by the event loop goroutine
- `cfg.Tmux` / `cfg.Persist` / `cfg.EventLog` / `cfg.Watcher`: backend interfaces (replaceable with fakes during testing)

### Communication Patterns

| Pattern | Direction | Characteristics | Example |
|---------|-----------|-----------------|---------|
| **Request-Response** | TUI → Server → TUI | Synchronous. Client blocks waiting on response ch | `switch-session`, `preview-session` |
| **Event Broadcast** | Server → all clients | Asynchronous. Delivered to all subscribed clients | `sessions-changed`, `project-selected`, `pane-focused` |
| **Tool Launch** | TUI → Server → tmux popup → Palette → Server | Indirect communication. Popup sends commands as an independent client | `new-session` |

`SessionInfo` is a unified type that carries static metadata and dynamic state in a single message: the runtime's `broadcastSessionsChanged` retrieves status / title etc. from each Session's `Driver.View(sess.Driver)` and packs them into `proto.SessionInfo`. `reduceTick` emits `EffBroadcastSessionsChanged` on every tick, delivering to all subscribers.

Responses are sent uniformly via the `sendResponse` method. Broadcasts are delivered only to clients that have sent the `subscribe` command.

### Message Format

All messages are represented as `proto.Envelope` structs, serialized as newline-delimited JSON (NDJSON). The `Type` field discriminates the message type.

| Field | Purpose |
|-------|---------|
| `type` | `"cmd"` / `"resp"` / `"evt"` |
| `req_id` | Correlates request-response pairs |
| `cmd` | Command name (when type=cmd) |
| `name` | Event name (when type=evt) |
| `status` | `"ok"` / `"error"` (when type=resp) |
| `data` | Typed payload (`json.RawMessage`) |
| `error` | Error details (when status=error) |

Command / Response / ServerEvent are closed sum types. See [interfaces.md](interfaces.md#interfaces) for detailed Go type definitions.

### Commands (Client → Server)

Only 3 wire command types exist. All domain operations are multiplexed through `CmdEvent`:

| Wire Command | Parameters | Function |
|---------|------------|----------|
| `subscribe` | filters (optional) | Start receiving broadcasts |
| `unsubscribe` | - | Stop receiving broadcasts |
| `event` | event, timestamp, sender_id, payload | Unified event envelope (see below) |

#### Event Types (via `CmdEvent.Event`)

Domain operations and driver hooks are dispatched via `CmdEvent`. TUI/tool operations are registered via `RegisterEvent[T]` and dispatched to typed handlers. Driver hook events (those with `SenderID` set) are routed as `EvDriverEvent` to the owning frame's driver — `SenderID` is the frame id read from the hook bridge's own pane environment.

| Event Type | Payload | Function |
|------------|---------|----------|
| `create-session` | project, command, options | Create a new session (root frame). `options` normalizes driver-agnostic launch flags such as `worktree.enabled` |
| `push-driver` | session_id, project, command, options | Append a new driver frame on top of an existing session's active frame |
| `stop-session` | session_id | Stop a session (terminates every frame in its stack) |
| `list-sessions` | - | Retrieve session list |
| `preview-session` | session_id | Preview in Pane 0.0 |
| `preview-project` | project | Stash the active session and broadcast `project-selected` event |
| `switch-session` | session_id | Switch to Pane 0.0 + focus |
| `focus-pane` | pane | Focus pane. Broadcasts `pane-focused` event |
| `launch-tool` | tool | Launch palette popup |
| `shutdown` | - | Shutdown all |
| `detach` | - | Detach |
| *(driver hooks)* | driver-specific | Hook events from agent (e.g., `state-change`, `session-start`). `SenderID` is the frame id; the reducer locates the owning frame across all sessions and routes the hook to that frame's driver |

### Client Message Routing

```mermaid
flowchart LR
    sock[Unix socket receive] --> decode[json.Decode]
    decode --> check{msg.Type?}
    check -->|response| resp[responses ch] --> cmd[Received by the sending Cmd]
    check -->|event| evt[events ch] --> listen["listenEvents() converts to tea.Msg"]
```

### Concurrency Model — Single Event Loop + Worker Pool

The server side of agent-roost is composed of a **single event loop + fixed-size worker pool**. All domain state (`state.State`) is solely owned by the event loop goroutine, and state transitions are expressed as the pure function `state.Reduce(state, event) → (state', []Effect)`. No `sync.Mutex` exists in the domain layer (except inside the worker pool).

#### Event Loop and State Ownership

```
runtime.Runtime.Run() — single goroutine
├── select {
│   ├── eventCh     — Events from IPC reader / event bridge
│   ├── internalCh  — conn open/close (runtime internal events)
│   ├── ticker.C    — EvTick at 1-second intervals
│   ├── workers.Results() — EvJobResult from worker pool
│   └── watcher.Events()  — EvFileChanged from fsnotify
│   }
├── dispatch(ev):
│   ├── state.Reduce(r.state, ev) → (next, effects)
│   ├── r.state = next
│   └── for _, eff := range effects { r.execute(eff) }
└── state: state.State (Sessions, Active, Subscribers, Jobs, ...)
    → solely owned by event loop goroutine. No mutex needed
```

#### Effect Interpreter Dispatch

`runtime.execute(eff)` maps each Effect type to backend I/O. Since Effect is a closed sum type, all side effects can be enumerated via `grep`:

```mermaid
flowchart LR
    classDef sync fill:#e8f0ff,stroke:#4060a0,stroke-width:2px
    classDef async fill:#f0f0f0,stroke:#888,stroke-dasharray:3 3

    Reduce["state.Reduce<br/>(pure function)"]:::sync
    Interp["Effect interpreter<br/>runtime.execute"]:::sync

    Reduce -->|"[]Effect"| Interp

    Interp -->|EffSpawnTmuxWindow| Tmux["tmux backend<br/>(go async spawn)"]:::async
    Interp -->|EffKillSessionWindow<br/>EffActivateSession<br/>EffDeactivateSession<br/>EffSelectPane<br/>EffSyncStatusLine<br/>EffRegisterPane<br/>EffUnregisterPane| TmuxSync["tmux backend<br/>(inline sync)"]:::sync
    Interp -->|EffSendResponse<br/>EffSendResponseSync<br/>EffBroadcastSessionsChanged| IPC["IPC conn writer"]:::sync
    Interp -->|EffPersistSnapshot| Persist["sessions.json writer"]:::sync
    Interp -->|EffStartJob| Pool["Worker pool<br/>(4 goroutine)"]:::async
    Interp -->|EffWatchFile| Watcher["fsnotify watcher"]:::sync
    Interp -->|EffEventLogAppend| EventLog["event log writer"]:::sync

    Tmux -->|EvTmuxPaneSpawned| EL["event loop (eventCh)"]
    Pool -->|EvJobResult| EL
    Watcher -->|EvFileChanged| EL
```

Legend:
- **Solid border** = executed synchronously on the event loop goroutine
- **Dashed border** = executed asynchronously in a separate goroutine. Results are fed back to the event loop as Events

**`EffSendResponse` vs `EffSendResponseSync`**: the former enqueues the wire frame on the connection's writer-goroutine outbox and returns immediately. The latter writes directly to the socket from the event loop goroutine, guaranteeing the response reaches the kernel buffer before the next effect in the same Reduce cycle runs. `reduceDetach` uses the sync form so the response lands before the following `EffDetachClient` tears the connection down.

#### Worker Pool (Off-Loop Execution of Slow I/O)

Heavy I/O (transcript parse, haiku summary, git branch detect, capture-pane) is executed outside the event loop in a fixed-size worker pool (`worker.Pool`, 4 goroutines). Runners are registered via `RegisterRunner[In,Out]` by the driver at init time, and `Dispatch` looks them up by `JobInput.JobKind()`:

```mermaid
sequenceDiagram
    participant EL as Event loop
    participant Red as state.Reduce
    participant Interp as Effect interpreter
    participant Pool as Worker pool (4 goroutine)
    participant Runner as Runner<br/>(TranscriptParse / Haiku / Git / CapturePane)

    EL->>Red: Reduce(state, EvTick)
    Red-->>EL: (state', [EffStartJob{input: CapturePaneInput}])
    EL->>Interp: execute(EffStartJob)
    Interp->>Pool: Submit(Job{ID, Input})
    Note over EL: event loop proceeds immediately to next select

    Pool->>Runner: Dispatch(pool, jobID, input)<br/>runner lookup by JobKind()
    Runner-->>Pool: (CapturePaneResult, nil)
    Pool->>EL: EvJobResult{JobID, Result}

    EL->>Red: Reduce(state, EvJobResult)
    Red-->>EL: (state', effects)<br/>DriverState updated via Driver.Step
```

Key points:
- **The event loop never blocks**: EffStartJob only submits to the worker pool. Results return asynchronously as EvJobResult
- **Fixed goroutine count**: event loop (1) + IPC accept (1) + worker pool (4) + IPC reader/writer (per client). Independent of session count
- **Type-based runner registration**: `worker.RegisterRunner("capture_pane", runner)` — adding a new job type requires only one RegisterRunner call + a runner function + a JobKind() method

#### Tick Processing Sequence

On each tick, `state.Reduce` calls Driver.Step for all sessions and returns the necessary Effects (capture-pane job, transcript parse job, broadcast, persist):

```mermaid
sequenceDiagram
    participant Tk as ticker.C
    participant EL as Event loop
    participant Red as state.Reduce
    participant D1 as Driver.Step (sess1)
    participant D2 as Driver.Step (sess2)
    participant Interp as Effect interpreter

    Tk->>EL: EvTick{Now}
    EL->>Red: Reduce(state, EvTick)
    Note over Red: reduceTick → stepAllSessions
    Red->>D1: Driver.Step(driverState1, DEvTick{Active: true})
    D1-->>Red: (driverState1', [EffStartJob{TranscriptParse}], view1)
    Red->>D2: Driver.Step(driverState2, DEvTick{Active: false})
    D2-->>Red: (driverState2', [EffStartJob{CapturePane}], view2)
    Red-->>EL: (state', [EffStartJob×N, EffBroadcastSessionsChanged,<br/>EffPersistSnapshot, EffSyncStatusLine])
    EL->>Interp: execute(effects...)
    Note over Interp: Interprets each Effect in order<br/>EffStartJob → worker pool submit<br/>EffBroadcast → IPC send<br/>EffPersist → write sessions.json
```

#### Hook Event Routing

Hook events are processed in a straight line: IPC reader → event loop → Reduce → Driver.Step. See [state-monitoring.md](state-monitoring.md#hook-event-routing-and-race-free-identification) for details.

#### Resident Goroutines

| Goroutine | Count | Role |
|-----------|-------|------|
| `Runtime.Run` (event loop) | 1 | State ownership + Reduce + Effect interpretation |
| `acceptLoop` | 1 | Accepts new connections from the unix socket |
| `ipcConn.readLoop` | M (1 / client) | IPC reader. Converts Commands to Events and submits to eventCh |
| `ipcConn.writeLoop` | M (1 / client) | IPC writer. Drains outbox and writes to socket |
| `worker.Pool.run` | 4 (fixed) | Worker pool goroutines |

Only IPC reader/writer scales with client count (one per TUI client). No per-session goroutines exist.

#### Hook Event Routing Sequence

```mermaid
sequenceDiagram
    participant Bridge as roost event <eventType><br/>(hook bridge)
    participant Reader as IPC reader goroutine
    participant EL as Event loop
    participant Red as state.Reduce
    participant Drv as Driver.Step<br/>(claudeDriver)

    Note over Bridge: SenderID is read from<br/>the pane environment (frame id)
    Bridge->>Reader: proto.CmdEvent{Event, Timestamp, SenderID, Payload}
    Reader->>EL: EvDriverEvent (eventCh)
    EL->>Red: Reduce(state, EvDriverEvent)
    Note over Red: reduceDriverHook: locate the owning frame<br/>→ Driver.Step(frame.Driver, DEvHook{...})
    Red->>Drv: Step(prev, DEvHook{Event, Payload})
    Drv-->>Red: (next, [EffEventLogAppend, EffStartJob{Haiku}], view)
    Red-->>EL: (state', effects + EffSendResponse + EffBroadcastSessionsChanged)
    EL->>EL: execute(effects...)
```

### IPC Type Design Invariants

`Cmd*`, `Resp*`, and `Evt*` types in `src/proto/` follow these invariants:

- **Optional fields use `omitempty`; zero value means absent.** A zero value with distinct semantics belongs in a separate type.
- **Names are client-agnostic.** No TUI- or GUI-specific terms in field or type names; both clients consume the same types.
- **Every concrete type carries its marker methods** (`isCommand()` / `CommandName()`, `isEvent()` / `EventName()`, `isResponse()`).
- **`state.View` is written by the driver only.** TUI and future GUI clients read state; neither branches on driver or connector name (see Driver/Connector isolation in `ARCHITECTURE.md`).

The flat `Cmd*`/`Evt*` naming is transitional; a future JSON-RPC migration will introduce `system.*` / `workspace.*` / `surface.*` / `notification.*` / `driver.*` namespaces.

## Tool System

High-level user operations are abstracted as `Tool`s. Executable from the same interface via both TUI and palette.

```go
// tools/tools.go
type Tool struct {
    Name        string
    Description string
    Params      []Param
    Run         func(ctx *ToolContext, args map[string]string) (*ToolInvocation, error)
}

type Param struct {
    Name    string
    Options func(ctx *ToolContext) []string  // generates choices at runtime
}

type ToolContext struct {
    Client *proto.Client   // typed IPC connection to daemon
    Config ToolConfig      // palette config (commands, projects)
    Args   map[string]string
}
```

### Tool to IPC Command Mapping

A Tool's `Run` sends typed IPC commands via `ToolContext.Client` (`proto.Client`). Each Tool corresponds to one IPC command. By returning a `ToolInvocation`, tool chaining within the same popup (e.g., create-project → new-session) is achieved.

| Tool | IPC Command | Parameters |
|------|-------------|------------|
| `new-session` | `create-session` | project, command |
| `stop-session` | `stop-session` | session_id |
| `detach` | `detach` | - |
| `shutdown` | `shutdown` | - |

Tools target high-level operations with side effects (create, stop, shutdown, etc.). Low-level navigation operations such as `switch-session`, `preview-session`, and `focus-pane` bypass Tools and are sent directly as IPC commands by the TUI.

### Parameter Completion via Palette

The palette is an independent process launched as a tmux popup. It does not block the TUI's event loop, and a crash does not affect the TUI.

Completion flow: tool selection → dynamically generate choices via each `Param`'s `Options` callback → incremental filtering by user input → execute `Tool.Run` after all parameters are resolved. Results reach the TUI via broadcast.
