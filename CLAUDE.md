# Agent Roost

## ビルド

```bash
make build    # ./roost を生成
make install  # ~/.local/bin/roost にインストール
make vet      # 静的解析
cd src && go test ./...
```

## コードルール

- TUI にビジネスロジックを実装しない。ソケット経由でサーバーに委譲
- 全操作は Tool として抽象化
- tmux 操作はインターフェース経由（PaneOperator, PaneCapturer）
- ファイルパスは Config.DataDir で注入
- 1ファイル 500 行以下、関数は 50 行以下
- 積極的にライブラリを使う。自前実装しない
- ユーザーの設定ファイル (~/.config/roost/) を上書きしない
