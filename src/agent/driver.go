package agent

// Driver はエージェントコマンド固有のふるまいを定義するインターフェース。
// 実装はすべて純粋関数的で I/O を持たない。
type Driver interface {
	Name() string
	PromptPattern() string
	DisplayName() string
}
