# Agent Roost

## ビルド

```bash
make build    # ./roost を生成
make install  # ~/.local/bin/roost にインストール
make vet      # 静的解析
cd src && go test ./...
```

## コードルール

- 設計原則は ARCHITECTURE.md に従う
- 1ファイル 500 行以下、関数は 50 行以下
- 積極的にライブラリを使う。自前実装しない
- ユーザーの設定ファイル (~/.config/roost/) を上書きしない
