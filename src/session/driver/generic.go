package driver

import "io/fs"

// Generic は bash, codex, gemini などの汎用コマンドを実装する。
// プロンプトパターンは $, >, ❯ の行末または行頭を対象とする。
type Generic struct {
	name string
}

const genericPromptPattern = `(?m)(^>|[>$❯]\s*$)`

func (g Generic) Name() string          { return g.name }
func (g Generic) PromptPattern() string { return genericPromptPattern }
func (g Generic) DisplayName() string   { return g.name }

func (g Generic) ResolveTitle(fsys fs.FS, projectPath string) string {
	return ""
}

// NewGeneric は任意のコマンド名に対する汎用 Driver を返す。
func NewGeneric(name string) Driver {
	return Generic{name: name}
}
