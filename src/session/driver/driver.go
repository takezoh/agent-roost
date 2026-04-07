package driver

import "io/fs"

// SessionMeta はドライバが解決するセッションのメタ情報。
type SessionMeta struct {
	Title      string // セッション名（例: custom-title）
	LastPrompt string // 直近のプロンプトテキスト
}

// Driver はエージェントコマンド固有のふるまいを定義するインターフェース。
type Driver interface {
	Name() string
	PromptPattern() string
	DisplayName() string
	ResolveMeta(fsys fs.FS, projectPath string) SessionMeta
}
