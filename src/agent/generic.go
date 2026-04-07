package agent

// genericDriver は bash, codex, gemini などの汎用コマンドを実装する。
// プロンプトパターンは $, >, ❯ の行末または行頭を対象とする。
type genericDriver struct {
	name string
}

const genericPromptPattern = `(?m)(^>|[>$❯]\s*$)`

func (g genericDriver) Name() string          { return g.name }
func (g genericDriver) PromptPattern() string { return genericPromptPattern }
func (g genericDriver) DisplayName() string   { return g.name }

// NewGenericDriver は任意のコマンド名に対する汎用 Driver を返す。
func NewGenericDriver(name string) Driver {
	return genericDriver{name: name}
}
