## Build & Test

```sh
make build          # src/ 以下を Go ビルド → ./roost
make vet            # go vet ./...
cd src && go test ./...  # 全テスト実行
```

## Rules

- 設計原則は ARCHITECTURE.md に従う
- 1ファイル 500 行以下、関数は 50 行以下
- 積極的にライブラリを使う。自前実装しない
- ユーザーの設定ファイル (~/.roost/) を上書きしない
- 機能追加・バグ修正には必ずテストを書く。テストなしで完了としない
