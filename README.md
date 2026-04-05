# Agent Roost

複数の Claude Code セッションを tmux 上で一元管理する TUI ツール。

## 特徴

- tmux のセッション管理をそのまま活用し、Claude Code の UI がそのまま動作
- プロジェクト別にセッションをグルーピング表示
- メインペインに常時フォーカス、`prefix Space` で TUI にトグル
- カーソル移動でセッションをメインペインにプレビュー
- セッション一覧は終了不可のサーバー。crash 時は自動復帰
- `remain-on-exit` でペイン終了時もレイアウト維持

## レイアウト

```
┌───────────────────┬────────┐
│                   │ TUI    │
│  Pane 0: Claude   │ ▼ atlas│
│  (常時フォーカス)  │  #1 ● │
│                   │  #2 ◆ │
├───────���───────────┤ ▼ forge│
│  Pane 1: ログ     │  #1 ○ │
└───────────────────┴────────┘
```

## 要件

- Go 1.22+
- tmux 3.2+

## インストール

```bash
make install
```

`~/.local/bin/roost` にインストールされます。

## 使い方

```bash
roost
```

tmux セッションを作成（または既存にアタッチ）し、3ペインレイアウトで起動します。

### prefix キー

デフォルト: `Ctrl+b`（tmux と同じ）。config で変更可能。

| キー | アクション |
|------|-----------|
| `prefix Space` | メインペイン ↔ TUI をトグル |
| `prefix d` | detach（tmux 生存、`roost` 再実行で復帰） |
| `prefix q` | 全終了（tmux セッション消滅） |

### TUI キーバインド（TUI フォーカス時）

| キー | アクション |
|------|-----------|
| `j`/`k` or `↑`/`↓` | セッション選択（メインペインにプレビュー） |
| `Enter` | 選択セッションに切替 → メインに戻る |
| `n` | クイック起動（デフォルトコマンド） |
| `N` | コマンド選択起動 |
| `p` | プロジェクト追加 |
| `d` | セッション停止（確認あり） |
| `Tab` | プロジェクト折りたたみ |

### セッション状態

| 表示 | 状態 |
|------|------|
| `●` 緑 | 稼働中（出力中） |
| `◆` 黄 | 待機中（入力待ち） |
| `○` 灰 | アイドル（30秒以上無出力） |
| `■` 赤 | 停止 |

## 設定

```toml
# ~/.config/roost/config.toml

[tmux]
session_name = "roost"
prefix = "C-Space"              # prefix キー（デフォルト: C-b）
pane_ratio_horizontal = 85      # メインペイン幅 %
pane_ratio_vertical = 80        # メインペイン高さ %

[monitor]
poll_interval_ms = 1000
idle_threshold_sec = 30

[session]
auto_name = true
default_command = "claude"
commands = ["claude", "gemini", "codex"]

[projects]
project_roots = ["~/dev", "~/work"]
```

設定ファイルがなくてもデフォルト値で動作します。
