package agent

// claudeDriver は Claude Code CLI のふるまいを実装する。
// プロンプトパターンは ❯ または > のみ（bash の $ との誤検知を避ける）。
type claudeDriver struct{}

const claudePromptPattern = `(?m)(^>|❯\s*$)`

func (claudeDriver) Name() string          { return "claude" }
func (claudeDriver) PromptPattern() string { return claudePromptPattern }
func (claudeDriver) DisplayName() string   { return "claude" }
