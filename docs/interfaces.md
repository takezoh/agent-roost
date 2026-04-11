# インターフェース・データファイル・ファイル構成

## インターフェース

テスト可能性のために state 層・runtime 層・driver 層をすべてインターフェース化。state 層は純粋な値型と pure function で構成され、runtime 層は backend interface 経由でテスト時に fake 差し替えが可能。

```go
// state/state.go — 全ドメイン状態 (plain data, no methods)
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
    Driver    DriverState   // sum type: driver impl ごとの concrete state
}
```

```go
// state/driver_iface.go — Driver interface (値型 plugin)
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

// DriverState — per-session state の closed sum type marker
type DriverState interface {
    driverStateMarker()
}

// DriverEvent — Driver.Step への入力 (closed sum type)
// DEvTick, DEvHook, DEvJobResult, DEvFileChanged
```

Driver は**値型 plugin**: goroutine なし、I/O なし、mutex なし。per-session state は `DriverState` 値として `state.Session.Driver` に埋め込まれ、`Driver.Step` の引数と戻り値として round-trip する。副作用は `[]Effect` として返し、runtime の Effect interpreter が実行する。

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
// state/reduce.go — 純粋状態遷移関数
func Reduce(s State, ev Event) (State, []Effect)
```

`Reduce` が全状態遷移の唯一のエントリポイント。Event / Effect は closed sum type (`isEvent()` / `isEffect()` marker)。

```go
// runtime/runtime.go — Imperative shell
type Runtime struct {
    cfg     Config
    state   state.State        // event loop goroutine が単独所有
    eventCh chan state.Event    // 外部 goroutine からの Event 投入
    workers *worker.Pool
    conns   map[state.ConnID]*ipcConn
    // ...
}

func (r *Runtime) Run(ctx context.Context) error  // event loop
func (r *Runtime) Enqueue(ev state.Event)          // goroutine-safe
```

```go
// runtime/backends.go — テスト差し替え可能な backend interface
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

// Command / Response / ServerEvent は closed sum type
type Command interface { isCommand(); CommandName() string }
```

driver-specific な hook payload は `proto.CmdEvent{Driver, Event, SessionID, Payload}` として typed IPC を渡る。各 driver subcommand (`roost event <eventType>` 等) が自分の hook payload を `CmdEvent` に詰め替えて送信し、runtime の IPC reader が `EvEvent` Event に変換して event loop に投入する。`reduceEvent` が `Sessions[ev.SessionID]` で 1 段ルックアップして `Driver.Step(driverState, DEvHook{...})` を呼ぶだけ。state 層も runtime 層も driver 固有のキー名を一切ハードコードしない。

`Driver.SpawnCommand` は Cold start 復元時に `runtime.Bootstrap` から呼ばれ、ドライバごとに固有の resume 方法でコマンド文字列を組み立てる。Claude ドライバは `Restore` で受け取った `session_id` を DriverState に保持しており、`lib/claude/cli.ResumeCommand` に委譲して `claude --resume <id>` を返す。Generic ドライバは base コマンドをそのまま返す。

## データファイル

| パス | 形式 | 内容 | ライフサイクル |
|------|------|------|--------------|
| `~/.roost/config.toml` | TOML | ユーザー設定（下記参照） | ユーザーが作成。存在しなければデフォルト値で動作 |
| `~/.roost/sessions.json` | JSON | セッション静的メタデータと Driver の `driver_state` (opaque map。status を含む) — 唯一の永続化先 (Single persistence store) | `EffPersistSnapshot` で書き出し (Tick / Hook event / session lifecycle 変更時)。読まれるのは daemon 起動時の `runtime.Bootstrap` のみ。`driver_state` の中身は driver が解釈する opaque な key/value で、runtime は key 名を一切知らない |
| `~/.roost/events/{sessionID}.log` | テキスト | エージェント hook イベントログ | `EffEventLogAppend` で追記。runtime の EventLogBackend が lazy-open でファイルハンドルを管理 |
| `~/.roost/roost.log` | slog | アプリケーションログ | daemon 起動時に作成/追記 |
| `~/.roost/roost.sock` | Unix socket | プロセス間通信 | daemon 起動時に作成。終了時に削除 |

`Config.DataDir` でベースパスを変更可能（テスト時に TempDir 指定）。

`settings.toml` の全フィールド（括弧内はデフォルト値）:

- `tmux`: `session_name` (`"roost"`), `prefix` (`"C-b"`), `pane_ratio_horizontal` (`75`), `pane_ratio_vertical` (`70`)
- `monitor`: `poll_interval_ms` (`1000`), `idle_threshold_sec` (`30`)
- `session`: `auto_name` (`true`), `default_command` (`"claude"`), `commands` (`["claude","gemini","codex"]`)
- `projects`: `project_roots` (`["~/dev","~/work"]`)

## ファイル構成

```
src/
├── main.go              daemon / TUI モード分岐 (lib.Dispatch でサブコマンド委譲)
├── state/               純粋ドメイン層 (no I/O, no goroutine)
│   ├── state.go         State, Session, Subscriber, JobMeta — plain value types
│   ├── event.go         Event closed sum type (EvCmdCreateSession, EvTick, EvJobResult, ...)
│   ├── effect.go        Effect closed sum type (EffSpawnTmuxWindow, EffStartJob, EffBroadcast, ...)
│   ├── reduce.go        Reduce(State, Event) → (State, []Effect) — 純粋状態遷移関数
│   ├── reduce_session.go  session lifecycle reducers
│   ├── reduce_hook.go   hook event → Driver.Step routing
│   ├── reduce_tick.go   EvTick → stepAllSessions → Driver.Step(DEvTick)
│   ├── reduce_job.go    EvJobResult → Driver.Step(DEvJobResult)
│   ├── reduce_conn.go   IPC connection lifecycle
│   ├── reduce_lifecycle.go  shutdown / detach
│   ├── driver_iface.go  Driver interface (Step, View, Persist, Restore, SpawnCommand)
│   │                    DriverState / DriverEvent / DriverStateBase marker
│   ├── status.go        Status 列挙型 (Running/Waiting/Idle/Stopped/Pending)
│   ├── view.go          View / Card / Tag — TUI 向け表示値型
│   ├── clone.go         State の copy-on-write ヘルパー
│   └── driver/          Driver 実装 — 値型 plugin (goroutine なし, I/O なし)
│       ├── claude.go    claudeDriver — event 駆動 status + transcript job emit
│       ├── claude_event.go  DEvHook dispatch (state-change, session-start, ...)
│       ├── claude_tick.go   DEvTick: active gate + transcript parse job emit
│       ├── claude_view.go   View() — Card, LogTabs, InfoExtras, StatusLine
│       ├── claude_persist.go  Persist / Restore — opaque bag round-trip
│       ├── generic.go   genericDriver — polling 駆動 (capture-pane job emit + hash 比較)
│       ├── generic_view.go  View()
│       ├── jobs.go      Job input/output 型 (TranscriptParseInput, CapturePaneInput, ...)
│       ├── poll.go      capture-pane driver 共通ヘルパー
│       ├── tags.go      CommandTag ヘルパー
│       └── register.go  init() で state.Register
├── runtime/             Imperative shell — event loop + Effect interpreter
│   ├── runtime.go       Runtime.Run() — single event loop (select)
│   ├── interpret.go     execute(Effect) — 全副作用の interpreter
│   ├── ipc.go           IPC server (accept, readLoop, writeLoop)
│   ├── backends.go      TmuxBackend, PersistBackend, EventLogBackend, FSWatcher interface
│   ├── tmux_real.go     TmuxBackend 具象実装
│   ├── persist.go       PersistBackend 具象実装 (sessions.json)
│   ├── eventlog.go      EventLogBackend 具象実装
│   ├── fsnotify.go      FSWatcher 具象実装
│   ├── proto_bridge.go  proto.Command → state.Event 変換
│   ├── bootstrap.go     warm/cold restart の初期 State 構築
│   ├── filerelay.go     ファイルリレー
│   ├── testing.go       テスト用 helper (fake backend)
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
│   ├── client.go        proto.Client (TUI / パレット / hook bridge 用)
│   ├── client_helpers.go  typed helper (CreateSession, StopSession, ...)
│   ├── convert.go       state.View → proto.SessionInfo 変換
│   ├── reqid.go         request ID 生成
│   └── errors.go        ErrCode enum
├── tools/
│   └── tools.go         Tool + Param + ToolContext + Registry + DefaultRegistry
├── lib/
│   ├── subcommand.go    サブコマンドレジストリ (Register, Dispatch)
│   ├── git/
│   │   └── git.go       git ブランチ検出 (DetectBranch)
│   └── claude/
│       ├── command.go   Claude サブコマンドハンドラ (init で "claude" 登録)
│       ├── hook.go      Claude hook イベントのパース
│       ├── setup.go     Claude settings.json への hook 登録/解除
│       ├── transcript/  Claude JSONL トランスクリプトのパース + 差分追跡
│       └── cli/         Claude CLI 起動コマンド組立て (ResumeCommand など)
├── config/
│   └── config.go        TOML 設定読み込み
├── tmux/
│   ├── interfaces.go    PaneOperator
│   ├── client.go        tmux コマンドラッパー (具象実装)
│   └── pane.go          ペイン操作
├── tui/
│   ├── model.go         セッション一覧 Model (UI 状態のみ。state.Status を直接 import)
│   ├── view.go          セッション一覧レンダリング
│   ├── mouse.go         マウス入力ハンドラ
│   ├── keys.go          キーバインド定義 + キーボード入力ハンドラ
│   ├── main_model.go    メイン TUI Model
│   ├── main_view.go     メイン TUI レンダリング
│   ├── palette.go       コマンドパレット
│   └── log_model.go     ログ TUI (動的セッションタブ)
└── logger/
    └── logger.go        slog 初期化
```
