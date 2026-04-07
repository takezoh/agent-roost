package driver

import "io/fs"

// Driver はエージェントコマンド固有のふるまいを定義するインターフェース。
type Driver interface {
	Name() string
	PromptPattern() string
	DisplayName() string
	ResolveTitle(fsys fs.FS, projectPath string) string
}
