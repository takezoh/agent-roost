# Architecture

## ビジョン

- 複数の AI エージェントセッションを、プロジェクトを跨いで一元管理する操作パネル
- エージェント自体のオーケストレーションには踏み込まず、セッションのライフサイクル管理に徹する薄い TUI
- 最小操作でセッションの起動・切替ができる

## 設計原則

- **tmux ネイティブ**: tmux のセッション/window/pane をそのまま活用。エージェントの PTY を再実装しない
- **操作はすべてツール**: 全操作を Tool として抽象化。TUI・コマンドパレット・将来のエージェント SDK から同じ Tool を実行できる
- **TUI にビジネスロジックを置かない**: TUI は表示とキー入力のみ。ロジックは core.Service に集約
- **プロセスハンドラによるライフサイクル管理**: TUI サーバーの死活監視と自動復帰。終了判断はプロセスハンドラの責務
- **テスト可能な設計**: tmux 操作はインターフェース経由。ファイルパスは注入可能

## レイヤー構成

```
tui/       表示層 — UI 状態管理、レンダリング、キー入力ディスパッチ
core/      サービス層 — セッション切替/プレビュー、popup 起動、状態ポーリング
session/   データ層 — セッション CRUD、JSON 永続化、状態定義
tmux/      インフラ層 — tmux コマンド実行、インターフェース定義
config/    設定 — TOML 読み込み、DataDir 注入
```

依存方向: `tui → core → session, tmux`

## プロセスモデル

3つの実行モードを1つのバイナリで提供。

```
roost                    → プロセスハンドラ
roost --tui sessions        → セッション一覧サーバー (Pane 2)
roost --tui palette [flags] → コマンドパレット (tmux popup)
roost --tui log            → ログ TUI (Pane 1, 将来)
```

### プロセスハンドラ

tmux セッションのライフサイクルを管理する親プロセス。

```
runProcessHandler()
├── tmux セッション作成 or 復元
├── Manager, Monitor, Service 初期化
├── Unix socket サーバー起動 (~/.config/roost/roost.sock)
├── Monitor ポーリングループ起動 (states-updated を broadcast)
├── ヘルスモニタ goroutine 起動 (2秒間隔で Pane 2 死活監視)
├── tmux attach (ブロック)
└── attach 終了時
    ├── shutdown コマンド受信済み → kill-session + sessions.json クリア
    └── 通常 detach → 終了（tmux 生存）
```

### セッション一覧サーバー

Pane 2 で動作する Bubbletea TUI。ソケット経由でプロセスハンドラに接続。終了不可（Ctrl+C 無効）。crash 時はヘルスモニタが自動 respawn。Manager/Monitor を持たず、全操作をソケット経由で委譲。

### コマンドパレット

`prefix p` または TUI の `n`/`N`/`p`/`d` で tmux popup として起動。ソケット経由でコマンド送信。ツール選択 → パラメータ入力 → 実行 → 終了。

## tmux レイアウト

```
┌─────────────────────┬────────────────┐
│  Pane 0.0           │  Pane 0.2      │
│  メイン (常時focus)  │  TUI サーバー   │
│                     │                │
├─────────────────────┤                │
│  Pane 0.1           │                │
│  ログ (tail -f)     │                │
└─────────────────────┴────────────────┘

Window 0: 制御画面（3ペイン固定）
Window 1+: セッション（バックグラウンド、swap-pane で Pane 0.0 に表示）
```

- `remain-on-exit on` でペイン終了時もレイアウト維持
- ターミナルサイズを `term.GetSize()` で取得し `new-session -x -y` に渡す
- prefix テーブルの全デフォルトキーを無効化し、Space/d/q/p のみ登録

## プロセス間通信 (IPC)

Unix domain socket (`~/.config/roost/roost.sock`) による JSON メッセージング。

```
PH (サーバー) ←sock→ TUI (クライアント, 長期接続, broadcast 購読)
PH (サーバー) ←sock→ Palette (クライアント, 短期接続)
```

### メッセージ種別

| 種別 | 方向 | 送信先 | 用途 |
|------|------|--------|------|
| **Response** | サーバー → クライアント | コマンド送信元のみ | コマンドの応答 |
| **Broadcast** | サーバー → クライアント | subscribe 済み全員 | sessions-changed, states-updated |

Response は `sendResponse` メソッドで統一送信。Broadcast は `subscribe` コマンドを送信したクライアントのみに配信。

### コマンド (クライアント → サーバー)

| コマンド | パラメータ | 機能 |
|---------|-----------|------|
| `subscribe` | - | ブロードキャストの受信を開始 |
| `create-session` | project, command | セッション作成 |
| `stop-session` | session_id | セッション停止 |
| `list-sessions` | - | セッション一覧取得 |
| `preview-session` | session_id, active_window_id | Pane 0 にプレビュー |
| `switch-session` | session_id, active_window_id | Pane 0 に切替 + フォーカス |
| `focus-pane` | pane | ペインフォーカス |
| `shutdown` | - | 全終了 |
| `detach` | - | デタッチ |

## ツールシステム

すべての操作はツールとして抽象化。

```go
type Tool struct {
    Name        string
    Description string
    Params      []Param
    Run         func(ctx *ToolContext, args map[string]string) error
}
```

| Tool | Params | 機能 |
|------|--------|------|
| `new-session` | project, command | tmux window 作成 + sessions.json 永続化 |
| `add-project` | project | TUI の projects map に追加 |
| `stop-session` | session_id | tmux window 削除 + sessions.json 更新 |
| `detach` | - | tmux detach-client |
| `shutdown` | - | 全終了 |

TUI/パレットはツール実行をソケット経由でサーバーに委譲。パレットがパラメータ補完を担当。

## セッション切替

`core.Service` が `swap-pane -d` チェーンを `RunChain` でアトミック実行。

```
Preview(sess, active):
  1. swap-pane -d  Pane 0.0 ↔ 旧セッション (旧を戻す)
  2. swap-pane -d  Pane 0.0 ↔ 新セッション (新を表示)
  3. respawn-pane  Pane 0.1 にログ tail
  → フォーカスは TUI (0.2) に残す

Switch(sess, active):
  Preview と同じ + SelectPane(0.0) でメインにフォーカス
```

## 状態監視

`tmux/monitor.go` が `PaneCapturer` インターフェース経由で各セッション window を 1 秒間隔でポーリング。

```
capture-pane で最後5行取得 → SHA256 ハッシュ比較
├── ハッシュ変化 + プロンプト検出 → StateWaiting (◆ 黄)
├── ハッシュ変化 + 出力中 → StateRunning (● 緑)
├── ハッシュ不変 + 30秒以上 → StateIdle (○ 灰)
└── エラー → StateStopped (■ 赤)
```

## キーバインド

### prefix キー（tmux レベル）

| キー | アクション |
|------|-----------|
| `prefix Space` | Pane 0.0 ↔ Pane 0.2 フォーカストグル |
| `prefix d` | detach |
| `prefix q` | 全終了 |
| `prefix p` | コマンドパレット |

### TUI キー（Pane 0.2 フォーカス時）

| キー | アクション |
|------|-----------|
| `j`/`k`, `↑`/`↓` | カーソル移動 (プレビュー連動) |
| `Enter` | セッション切替 → メインにフォーカス |
| `n` | new-session (project=cursor, command=default) |
| `N` | new-session (project=cursor, command=prompt) |
| `p` | add-project |
| `d` | stop-session |
| `Tab` | プロジェクト折りたたみ |

### パレットキー（popup 内）

| キー | アクション |
|------|-----------|
| 文字入力 | fuzzy filter |
| `↑`/`↓`, `C-p`/`C-n` | カーソル移動 |
| `Enter` | 選択 |
| `Esc` | 閉じる / 戻る |

## インターフェース

テスト可能性のために tmux 操作をインターフェース化。

```go
// tmux/interfaces.go
type PaneOperator interface {
    SwapPane(src, dst string) error
    SelectPane(target string) error
    RespawnPane(target, command string) error
    RunChain(commands ...[]string) error
}

type PaneCapturer interface {
    CapturePaneLines(target string, n int) (string, error)
}
```

- `core.Service` → `PaneOperator` に依存
- `tmux.Monitor` → `PaneCapturer` に依存
- `session.Manager` → `TmuxClient` インターフェースに依存
- ファイルパスは `Config.DataDir` で注入

## データファイル

| パス | 形式 | 内容 |
|------|------|------|
| `~/.config/roost/config.toml` | TOML | ユーザー設定 |
| `~/.config/roost/sessions.json` | JSON | セッション一覧 |
| `~/.config/roost/logs/{id}.log` | テキスト | セッション別ログ |
| `~/.config/roost/roost.log` | slog | アプリケーションログ |
| `~/.config/roost/roost.sock` | Unix socket | プロセス間通信 |

`Config.DataDir` でベースパスを変更可能（テスト時に TempDir 指定）。

## ファイル構成

```
src/
├── main.go              プロセスハンドラ / モード分岐
├── core/
│   ├── server.go        Unix socket サーバー、コマンドハンドラ、broadcast
│   ├── client.go        ソケットクライアント（TUI・パレット用）
│   ├── protocol.go      メッセージ型定義 (Message, SessionInfo)
│   └── service.go       ビジネスロジック（切替、プレビュー、popup 起動）
├── config/
│   └── config.go        TOML 設定読み込み
├── session/
│   ├── manager.go       セッション CRUD + JSON 永続化
│   ├── state.go         状態 enum + Session struct
│   └── log.go           ログパス管理
├── tmux/
│   ├── interfaces.go    PaneOperator, PaneCapturer
│   ├── client.go        tmux コマンドラッパー（具象実装）
│   ├── pane.go          ペイン操作
│   └── monitor.go       状態監視
├── tui/
│   ├── model.go         Bubbletea Model（UI 状態のみ）
│   ├── view.go          レンダリング
│   ├── keys.go          キーバインド
│   ├── tool.go          ツール定義 + Registry
│   └── palette.go       コマンドパレット
└── logger/
    └── logger.go        slog 初期化
```

## 依存

| パッケージ | 用途 |
|-----------|------|
| `charm.land/bubbletea/v2` | TUI フレームワーク |
| `charm.land/lipgloss/v2` | スタイリング |
| `charm.land/bubbles/v2` | キーバインド |
| `github.com/BurntSushi/toml` | 設定ファイル |
| `golang.org/x/term` | ターミナルサイズ取得 |
| `log/slog` | 構造化ログ (標準ライブラリ) |
