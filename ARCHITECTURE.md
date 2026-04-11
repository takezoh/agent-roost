# Architecture

本ドキュメントは開発者向けに roost の内部アーキテクチャを説明する。

**roost** は tmux 上で複数の AI エージェントセッションを一元管理する TUI ツールである。

## 詳細ドキュメント

- [プロセスモデル・tmux レイアウト・描画責務](docs/process-model.md) — Daemon/TUI プロセス構成、ペインレイアウト、Driver と TUI の描画境界
- [プロセス間通信・ツールシステム](docs/ipc.md) — IPC メッセージ形式、コマンド一覧、並行性モデル (event loop + worker pool)、Tool 抽象
- [状態監視・UX 処理パイプライン](docs/state-monitoring.md) — Driver plugin による状態検出、Claude/Generic driver、永続化/復元、UX フロー
- [インターフェース・ファイルリファレンス](docs/interfaces.md) — Go 型定義、データファイル、ソースツリー

## ビジョン

AI エージェントを複数プロジェクトで並行稼働させると、tmux の素の操作ではセッションの把握・切替が煩雑になり、各エージェントが idle/running/waiting のどの状態かも見えない。これを解決する。

- 複数の AI エージェントセッションを、プロジェクトを跨いで一元管理する操作パネル
- エージェント自体のオーケストレーションには踏み込まず、セッションのライフサイクル管理に徹する薄い TUI
- 最小操作でセッションの起動・切替ができる

## 設計原則

- **Functional Core / Imperative Shell**: 全状態遷移は純関数 `state.Reduce(state, event) → (state', effects)` で表現する。I/O は `Effect` 値として external に出し、単一の event loop (runtime) が interpret する。goroutine・mutex・actor は state 層に存在しない
- **tmux ネイティブ**: tmux のセッション/window/pane をそのまま活用。エージェントの PTY を再実装しない
- **高レベル操作はツール**: セッション作成・停止・終了など、副作用を伴う高レベル操作を Tool として抽象化 (`tools` パッケージ)。TUI・コマンドパレットから同じ Tool を実行できる
- **TUI にビジネスロジックを置かない**: TUI は表示とキー入力のみ。ロジックは `state.Reduce` + `runtime` に集約
- **Typed messages**: 全境界 (IPC command/response/event, state Event/Effect, driver DriverEvent) は closed sum type。closure を message にしない
- **Driver は値型**: Driver は per-session state を持たない stateless plugin。per-session state は `DriverState` 値として `state.Session.Driver` に埋め込まれ、`Driver.Step` の引数と戻り値として round-trip する。goroutine も I/O も持たない
- **Effects are auditable**: 全 side-effect は `state.Effect` enum の constructor として `grep` 可能。driver が直接ファイルに書いたり subprocess を spawn したりしない — 全て runtime の interpreter が実行する
- **Single event loop**: daemon の全状態は 1 つの goroutine (runtime event loop) が単独所有する。mutex 不要 (worker pool 内部を除く)。session 数に依存しない固定 goroutine 数 (~16)
- **Worker pool for slow I/O**: transcript parse、haiku summary、git branch detect、capture-pane は fixed-size worker pool (4 goroutine) で off-loop 実行。結果は `EvJobResult` として event loop に feed back
- **Single persistence store**: `sessions.json` が唯一の真実。tmux user options は `@roost_id` (window-to-session marker) のみ。二重管理しない
- **フォールバック禁止**: 「情報源 A が無ければ B」という合成は行わない。Driver の Step が状態を更新しない限り、status は変わらない
- **テスト可能な設計**: state.Reduce は pure function test で 90%+ coverage。runtime は interface 経由の fake backend で E2E test。mock 不要
- **Driver 固有概念の隔離**: `driver/` と `lib/` の固有概念（型名、定数、インターフェイス、設定）は `state/`, `runtime/`, `tui/`, `proto/`, `config/` に露出させない。driver 固有の TabKind 定数は各 driver パッケージで定義する。driver 固有の config は `[drivers.<name>]` TOML セクションで driver が自身の型に decode する。main.go だけがワイヤリングとして driver/lib を import する
- **Connector 固有概念の隔離**: Driver と同じ原則を Connector にも適用する。`connector/` と `lib/` の固有概念は `state/`, `runtime/`, `tui/`, `proto/` に露出させない。TUI から connector 名で分岐しない (`if name == "github"` 禁止)。connector は I/O を持たない (Effect で runtime に委譲)。Job input/output 型と runner は `connector/` パッケージ内で定義・登録する
- **プラガブル登録パターン**: driver のプラグイン登録は `RegisterTabRenderer[C]`, `RegisterRunner[In,Out]`, `state.Register(Driver)` の 3 つの init 時レジストリで行う。connector のプラグイン登録は `RegisterRunner[In,Out]`, `state.RegisterConnector(Connector)` で行う。runtime/tui は具体的な driver/connector を知らずにレジストリ経由で dispatch する

## 用語

| 用語 | 意味 | tmux 上の実体 |
|------|------|--------------|
| **セッション** | AI エージェントの作業単位。`state.Session` (静的メタデータ + DriverState) を sessionID で管理する | tmux **window**（Window 1+、単一ペイン構成） |
| **制御セッション** | roost 全体を収容する tmux セッション | tmux **session**（`roost`） |
| **ペイン** | Window 0 内の制御ペイン | tmux **pane**（`0.0`, `0.1`, `0.2`） |
| **コネクター** | デーモン単位の外部サービス連携プラグイン。GitHub/Linear/Jira 等の外部サービスからデータを取得し TUI に表示する。Driver がセッション単位なのに対し、Connector はデーモン全体で各1インスタンス | なし（tmux リソースを持たない） |
| **Warm start (温起動)** | tmux session 生存状態での Runtime 起動。sessions.json + tmux `@roost_id` から状態を復元 | 既存の tmux session/window/pane を再利用 |
| **Cold start (冷起動)** | tmux session 消滅状態 (PC 再起動 / tmux server 死亡) での Runtime 起動。`sessions.json` から tmux session/window を再作成 | tmux session/window を新規作成 |

以降「セッション」は roost セッションを指す。tmux セッションには「tmux セッション」と明記する。

Runtime の起動は必ず Warm start か Cold start のどちらかで、初回起動という分岐は持たない (sessions.json が存在しなければ空のセッション一覧で Cold start するだけ)。

## レイヤー構成

```
state/         純粋ドメイン層 — State, Event, Effect, Reduce (no I/O, no goroutine)
driver/        Driver 実装 — 値型 Driver plugin + per-session DriverState。I/O 持たない
connector/     Connector 実装 — 値型 Connector plugin + per-daemon ConnectorState。I/O 持たない
runtime/       Imperative shell — 単一 event loop, Effect interpreter, backend 抽象
runtime/worker/ Worker pool — slow I/O job 実行 (haiku, transcript parse, git, capture-pane, github fetch)
proto/         Typed IPC — Command / Response / ServerEvent の sum type + wire codec
tools/         パレットツール — TUI 向け Tool 抽象 + DefaultRegistry
tui/           表示層 — Bubbletea UI 状態管理、レンダリング、キー入力
tmux/          インフラ層 — tmux コマンド実行ラッパー
lib/           ユーティリティ — 外部ツール連携 (lib/git/, lib/claude/, lib/github/)
config/        設定 — TOML 読み込み、DataDir 注入
logger/        ログ — slog 初期化、ログファイル管理
```

daemon プロセスと TUI プロセスは別プロセスで、Unix socket 経由の typed IPC (`proto` パッケージ) で通信する。

コード依存方向:
- `main` → `runtime`, `driver`, `connector`, `proto`, `tools`, `tmux`, `config`, `logger`
- `runtime` → `state` (Reduce 呼び出し), `proto` (wire encode/decode), `runtime/worker` (Pool + Dispatch)
- `runtime/worker` → `state` (JobID, JobInput, EvJobResult)。driver/connector/lib を import しない
- `state` は自己完結 — 外部パッケージを一切 import しない (pure functional core)
- `driver` → `state` (DriverStateBase embed, Effect/View 型), `runtime/worker` (RegisterRunner), `lib/*` (実装)
- `connector` → `state` (ConnectorStateBase embed, Effect 型), `runtime/worker` (RegisterRunner), `lib/*` (実装)
- `proto` → `state` (Status enum, View/ConnectorSection 型を wire に乗せる)
- `tools` → `proto` (Client 呼び出し)
- `tui` → `proto` (Client + SessionInfo + ConnectorInfo), `state` (Status/View/ConnectorSection/TabRenderer 型), `tools` (ToolRegistry)。driver/connector/lib を import しない
- `lib/claude/command.go` (hook bridge) → `proto` (CmdHook 送信), `config`
- `lib/claude/transcript` → `state` (RegisterTabRenderer で TabRenderer factory を登録)
- `lib/subcommand.go` でサブコマンドレジストリを提供。各 lib パッケージが `init()` で登録し、`main` は `lib.Dispatch` でディスパッチ
- `state.Session` が静的メタデータと DriverState (動的状態) を 1 つの struct に保持。Reduce が sessionID で routing し、Driver.Step に渡す
- `state.State.Connectors` がデーモン単位の ConnectorState を保持。Reduce が connector name で routing し、Connector.Step に渡す

## 設計判断

| 判断 | 選択 | 理由 |
|------|------|------|
| パレットの実装方式 | tmux popup (独立プロセス) | crash 分離。Bubbletea サブモデルでは TUI 内で panic を共有する |
| Ctrl+C の無効化 | KeyPressMsg を consume | 常駐プロセスの誤終了防止。ヘルスモニタの respawn まで操作不能になる |
| 楽観的更新をしない | IPC エラー時に UI 状態を変更しない | 次回ポーリングで自動回復。状態不整合のリスクを回避 |
| 状態遷移の表現 | 純関数 `state.Reduce(state, event) → (state', effects)` | 全状態遷移が 1 関数に集約され、テストは pure function test で 90%+ coverage。goroutine / channel / timing 依存なし |
| 並行性モデル | Single event loop + fixed-size worker pool | per-session goroutine を排除。goroutine 数は固定 (~16)。デッドロック不在 (actor 間通信が存在しない) |
| Driver の設計 | 値型 plugin: `Step(prev DriverState, ev DriverEvent) → (next, effects, view)` | goroutine なし、I/O なし、mutex なし。副作用は Effect 値として runtime に委譲。テストは入力と出力の比較だけで完結 |
| セッションメタデータの永続化 | `sessions.json` が唯一の永続化先 (Single persistence store)。tmux user options は `@roost_id` のみ | tmux user options に `@roost_persisted_state` を二重管理しない。`@roost_id` は window-to-session marker としてのみ使い、Driver 状態は `sessions.json` の `driver_state` bag に一元化 |
| shutdown (`C-b q`) の挙動 | `EffKillSession` のみで sessions.json は残す | 次回起動時に `runtime.Bootstrap` でセッションを復元できるようにするため |
| Cold start 復元時の Claude 起動コマンド | `claude --resume <id>` を `Driver.SpawnCommand` で組立て | 過去の会話 transcript を新しい Claude プロセスにそのまま引き継ぐ。Claude 固有の `--resume` フラグ知識は `lib/claude/cli` に閉じ、`driver/claude.go` から委譲する |
| swap-pane チェーンのロールバック | しない | tmux の `;` 連結はアトミックではなく途中ロールバック不可。Reduce は State を変更するだけで tmux を直接触らないので、Effect 失敗時も State の整合性は保たれる |
| IPC タイムアウト | 設定しない | event loop のデッドロックは daemon 全体の障害を意味し、Client 側のタイムアウトでは回復できない。外部からの再起動が唯一の復帰手段であるため優先度は低い |
| IPC wire format | `proto` パッケージの typed sum type (Command / Response / ServerEvent) + NDJSON envelope | 全境界メッセージが closed sum type で型安全。`{type, req_id, cmd|name, data}` envelope で拡張可能 |
| Session と Driver の責務分離 | `state.Session` が静的メタデータ + `DriverState` を 1 struct に保持。Reduce が sessionID で routing し Driver.Step に渡す | 1 つの session に対して 1 つの真実。Reduce の中で Session と DriverState が同時に更新されるので状態不整合が原理的に発生しない |
| エージェント状態検出 | Driver.Step が DEvHook / DEvTick / DEvJobResult を受けて DriverState を更新する純関数 | Observer 抽象は廃止。フォールバック禁止: Driver.Step が走らない限り status は変わらない |
| エージェントイベント連携 | `roost event <eventType>` → `proto.CmdEvent` → `EvEvent` → `reduceEvent` → `Driver.Step(DEvHook)` | hook bridge が `$ROOST_SESSION_ID` env var で race-free に sessionID を特定。reducer は `Sessions[ev.SessionID]` で 1 段ルックアップ。詳細は [hook event ルーティングと race-free identification](docs/state-monitoring.md#hook-event-ルーティングと-race-free-identification) |
| driver hook payload の抽象化 | `CmdHook.Payload` を不透明 `map[string]any` バッグとして運ぶ | 各 driver subcommand が tool 固有の hook field を CmdHook に packing する。reducer は中身を見ずに `DEvHook.Payload` として Driver.Step に転送するだけ。固有 field を増やしても state / runtime / proto には一切手が入らない |
| Session ランタイム情報の永続化 | `Driver.Persist(driverState)` が opaque `map[string]string` を返し、`EffPersistSnapshot` が sessions.json に書き出す | driver 固有のキーを増やしても runtime 層は触らない。永続化先は sessions.json の 1 箇所のみ |
| Connector のスコープ | デーモン単位 (各 Connector につき 1 インスタンス) | GitHub/Linear 等の外部サービス情報はセッション単位でもプロジェクト単位でもなく、ユーザーアカウント全体に紐づく。Driver (セッション単位) に組み込むと同一プロジェクトの複数セッションで重複取得が発生する。デーモン単位なら全セッションで 1 回の API コールで済む |
| Connector の状態永続化 | しない (キャッシュ TTL ベース) | Connector のデータは 2 分の TTL で再取得するため、再起動時に即座に取得し直せばよい。sessions.json に Connector 状態を混ぜると永続化スキーマが複雑化する |
| Connector の初期化タイミング | 最初の EvTick で ConnectorsReady フラグ付き | runtime.Bootstrap ではなく reducer 内で初期化することで、初期化ロジックが pure function test でカバーできる。ConnectorsReady bool フラグで冪等性を保証 |
| 動的ステータスの永続化 | Status は DriverState に含まれ、`Driver.Persist` → sessions.json → `Driver.Restore` で round-trip | Warm/Cold restart 後、`Driver.Restore(bag, now)` で前回値を復元。Idle にリセットされない |
| polling と event 駆動の統一インターフェース | `Driver.Step` が `DEvTick` (polling) と `DEvHook` (event) の両方を受ける | Claude の status は DEvHook 駆動 (新 event が来るまで status 不変)。Generic は DEvTick で capture-pane job を emit。reducer は Driver を区別せず `Driver.Step(driverState, driverEvent)` を呼ぶだけ。新 driver 追加は Driver interface を 1 つ実装すればよい |
| Driver の active 判定 | `DEvTick.Active` フラグで push | 真実は `state.State.Active` の 1 点のみ。reducer が DEvTick 構築時に `sess.WindowID == state.Active` を評価して Driver.Step に渡す。Driver 側は pull も callback も不要 |
| transcript パースの off-loop 実行 | Worker pool の `TranscriptParse` runner が `transcript.Tracker` を保持。Driver.Step は `EffStartJob{TranscriptParseInput}` を emit するだけ | event loop をブロックしない。結果は `EvJobResult{TranscriptParseResult}` で戻り、`Driver.Step(DEvJobResult)` で DriverState に反映。Tracker は worker pool 内に閉じ、state 層は transcript 形式を知らない |
| 初回 Tick で status を触らない | `genericDriver.Step(DEvTick)` は `primed=false` のとき hash baseline を設定するだけで status を更新しない | restart 直後の最初のポーリングで `Driver.Restore` で復元した status を上書きしないため。次の Tick で実際に hash 変化を観測したときだけ status を更新する |
| Worker pool の runner dispatch | `worker.Dispatch` が `JobInput.JobKind()` で登録済み runner を lookup して dispatch | switch 文不要。新 job 型の追加は `RegisterRunner[In,Out]` 1 行 + `JobKind()` メソッドだけ |
| StatusLine の表示 | Worker pool `TranscriptParse` runner → `TranscriptParseResult.StatusLine` → `DriverState` → `Driver.View().StatusLine` → `EffSyncStatusLine` → tmux status-left | transcript 差分読みから tmux 反映まで全て Effect 経由。state 層は tmux を直接触らない |

## 副作用の命名規約

パス計算と副作用を関数名で区別する。

| パターン | 副作用 | 例 |
|---------|--------|-----|
| `XxxPath()` | なし (純粋) | `LogDirPath`, `ConfigDirPath`, `LogPath` |
| `EnsureXxx()` | ディレクトリ作成 | `EnsureLogDir`, `EnsureConfigDir` |
| `LoadFrom(path)` | ファイル読込のみ | `config.LoadFrom` |
| `Load()` | ディレクトリ作成 + ファイル読込 | `config.Load` (convenience wrapper) |

## テスト方針

テストファイルは対象ファイルと同じディレクトリに `*_test.go` として配置。

- **state.Reduce のテスト**: mock 不要。`Reduce(state, event)` の戻り値 `(state', effects)` を直接検証する pure function test。goroutine / channel / timing 依存なし
- **Driver.Step のテスト**: mock 不要。`Step(prev, driverEvent)` の戻り値 `(next, effects, view)` を直接検証
- **runtime のテスト**: backend interface の fake を注入。`runtime.Config` に `noopTmux` / `noopPersist` 等を設定してテスト。`Config.DataDir` に `t.TempDir()` を注入してファイル I/O を分離
- **TUI テスト**: Bubbletea の `Model.Update` にメッセージを直接渡し、返り値の Model 状態を検証。実際のターミナルは不要

## 依存

| パッケージ | バージョン | 用途 |
|-----------|-----------|------|
| `charm.land/bubbletea/v2` | v2.0.2 | TUI フレームワーク |
| `charm.land/lipgloss/v2` | v2.0.2 | スタイリング |
| `charm.land/bubbles/v2` | v2.1.0 | キーバインド |
| `github.com/BurntSushi/toml` | v1.6.0 | 設定ファイル |
| `github.com/fsnotify/fsnotify` | v1.9.0 | ファイル監視 |
| `golang.org/x/term` | v0.41.0 | ターミナルサイズ取得 |
