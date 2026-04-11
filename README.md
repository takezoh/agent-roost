# Agent Roost

複数の AI エージェントセッションを tmux 上で一元管理する TUI ツール。

## 特徴

- tmux のセッション管理をそのまま活用し、エージェントの UI がそのまま動作
- プロジェクト別にセッションをグルーピング表示
- メインペインに常時フォーカス、`prefix Space` で TUI にトグル
- カーソル移動でセッションをメインペインにプレビュー
- セッション一覧は終了不可のサーバー。crash 時は自動復帰
- `remain-on-exit` でペイン終了時もレイアウト維持

## レイアウト

```
┌───────────────────┬────────┐
│                   │ TUI    │
│  Pane 0: Agent    │ ▼ atlas│
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
| `prefix p` | コマンドパレット（ツール一覧を補完付きで表示） |

### コマンドパレット

`prefix p` でポップアップ表示。テキスト入力でツールを絞り込み、Enter で実行。

```
> new█
▸ new-session       セッション作成
  create-project    プロジェクト作成+セッション開始
```

| ツール | 説明 |
|--------|------|
| `new-session` | セッション作成（プロジェクト・コマンド選択） |
| `create-project` | プロジェクトディレクトリを作成しセッションを開始 |
| `stop-session` | セッションを停止 |
| `detach` | デタッチ（セッション維持） |
| `shutdown` | 全終了（セッション破棄） |

### TUI キーバインド（TUI フォーカス時）

| キー | アクション |
|------|-----------|
| `j`/`k` or `↑`/`↓` | セッション選択（メインペインにプレビュー） |
| `Enter` | 選択セッションに切替 → メインに戻る |
| `n` | クイック起動（デフォルトコマンド） |
| `N` | コマンド選択起動 |
| `d` | セッション停止（確認あり） |
| `Tab` | プロジェクト折りたたみ |
| `1`-`5` | ステータスフィルタ切替 |
| `0` | フィルタリセット |

### セッション状態

| 表示 | 状態 |
|------|------|
| `●` 緑 | 稼働中（出力中） |
| `◆` 黄 | 待機中（入力待ち） |
| `◇` 黄 | 承認待ち（ツール実行の許可待ち） |
| `○` 灰 | アイドル（30秒以上無出力） |
| `■` 赤 | 停止 |

## 設定

```toml
# ~/.roost/settings.toml

[tmux]
session_name = "roost"
prefix = "C-Space"              # prefix キー（デフォルト: C-b）
pane_ratio_horizontal = 75      # メインペイン幅 %（デフォルト: 75）
pane_ratio_vertical = 70        # メインペイン高さ %（デフォルト: 70）

[monitor]
poll_interval_ms = 1000
idle_threshold_sec = 30

[session]
auto_name = true
default_command = "claude"
commands = ["claude", "gemini", "codex"]

[session.aliases]
cc = "claude"

[projects]
project_roots = ["~/dev", "~/work"]
project_paths = ["~/dotfiles"]
```

設定ファイルがなくてもデフォルト値で動作します。
