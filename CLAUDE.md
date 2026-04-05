# Agent Roost

tmux ラッパー + Go(Bubbletea) TUI で複数の Claude Code セッションを管理するツール。

## アーキテクチャ

```
roost (プロセスハンドラ)
├── tmux セッション作成/復元
├── セッション一覧サーバー起動 (Pane 2)
├── ヘルスモニタ goroutine (Pane 2 の死活監視・自動 respawn)
└── tmux attach (ブロック)
    ├── prefix+d (detach) → PH 終了、tmux 生存
    └── prefix+q (終了) → PH が kill-session → 全終了

セッション一覧サーバー (Pane 2, ROOST_TUI=1)
├── セッション一覧表示・操作
├── 終了機能なし (Ctrl+C も無効)
└── crash → ヘルスモニタが自動 respawn
```

## 構成

```
src/           Go ソース (モジュールルート)
├── main.go    プロセスハンドラ + セッション一覧サーバー (2モード)
├── config/    ~/.config/roost/config.toml 読み込み
├── session/   セッション CRUD, 状態定義, ログ管理
├── tmux/      tmux コマンドラッパー, ペイン操作, 状態監視
└── tui/       Bubbletea Model, View, キーバインド, ダイアログ
```

## ビルド

```bash
make build    # ./roost を生成
make install  # ~/.local/bin/roost にインストール
make vet      # 静的解析
```

## 設計判断

- プロセスハンドラが tmux セッションのライフサイクルを管理
- セッション一覧は終了不可のサーバープロセス。crash 時はヘルスモニタが 2 秒以内に respawn
- 終了判断はプロセスハンドラの責務 (tmux 環境変数 ROOST_SHUTDOWN でフラグ管理)
- Claude Code の PTY を再実装せず、tmux pane に本物を表示。swap-pane -d で切替
- prefix テーブルは Space/d/q のみ。他の tmux キーは無効化しレイアウト崩壊を防止
- 新規セッション作成時、ターミナルサイズを term.GetSize() で取得し tmux に渡す
- session/manager.go は TmuxClient インターフェース経由で tmux に依存 (import cycle 回避)

## 依存

- charm.land/bubbletea/v2, lipgloss/v2, bubbles/v2
- github.com/BurntSushi/toml
- golang.org/x/term
- tmux 操作は os/exec 経由

## テスト

```bash
cd src && go test ./...
```
