# Agent Roost

tmux ラッパー + Go(Bubbletea) TUI で複数の AI エージェントセッションを管理するツール。

## アーキテクチャ

```
roost (プロセスハンドラ)
├── tmux セッション作成/復元
├── セッション一覧サーバー起動 (Pane 2)
├── ヘルスモニタ goroutine (Pane 2 の死活監視・自動 respawn)
└── tmux attach (ブロック)
    ├── prefix+d (detach) → PH 終了、tmux 生存
    └── prefix+q (終了) → PH が kill-session → 全終了

セッション一覧サーバー (Pane 2, roost --tui sessions)
├── セッション一覧表示・操作 (UI のみ、ビジネスロジックは core/)
├── 終了機能なし (Ctrl+C も無効)
└── crash → ヘルスモニタが自動 respawn
```

## レイヤー構成

```
tui/       表示層 — Bubbletea Model/View、キー入力 → ソケット経由でサーバーに委譲
core/      サービス層 — ソケットサーバー/クライアント、セッション切替、プレビュー、状態ポーリング
session/   データ層 — セッション CRUD、JSON 永続化、状態定義
tmux/      インフラ層 — tmux コマンド実行、インターフェース定義
config/    設定 — TOML 読み込み、DataDir 注入
logger/    ログ — slog ベース、ファイル出力
```

## ファイル構成

```
src/
├── main.go              プロセスハンドラ / モード分岐
├── core/
│   ├── server.go        Unix socket サーバー
│   ├── client.go        ソケットクライアント
│   ├── protocol.go      メッセージ型定義
│   └── service.go       ビジネスロジック
├── config/
│   └── config.go        TOML 設定読み込み、DataDir 注入
├── session/
│   ├── manager.go       セッション CRUD + JSON 永続化
│   ├── state.go         状態 enum + Session struct
│   └── log.go           ログパス管理
├── tmux/
│   ├── interfaces.go    PaneOperator, PaneCapturer インターフェース
│   ├── client.go        tmux コマンドラッパー（具象実装）
│   ├── pane.go          ペイン操作
│   └── monitor.go       状態監視 (PaneCapturer 依存)
├── tui/
│   ├── model.go         Bubbletea Model（UI 状態のみ）
│   ├── view.go          レンダリング
│   ├── keys.go          キーバインド
│   ├── tool.go          ツール定義 + Registry
│   └── palette.go       コマンドパレット
└── logger/
    └── logger.go        slog 初期化
```

## ビルド

```bash
make build    # ./roost を生成
make install  # ~/.local/bin/roost にインストール
make vet      # 静的解析
```

## 設計判断

- TUI にビジネスロジックを実装しない。ソケット経由でサーバーに委譲
- プロセスハンドラが Unix socket サーバーを運用。Manager/Monitor を一元管理
- TUI/パレットはソケットクライアント。データの重複保持なし
- Response (個別応答) と Broadcast (購読者への通知) を明確に分離
- 全操作は Tool として抽象化。TUI・パレット・将来の SDK から同じ Tool を実行
- tmux 操作はインターフェース経由（PaneOperator, PaneCapturer）。テスト時にモック可能
- ファイルパスは Config.DataDir で注入。テスト時に TempDir 指定可能
- prefix テーブルは Space/d/q/p のみ。他の tmux キーは無効化

## テスト

```bash
cd src && go test ./...
```
