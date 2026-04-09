package tmux

type PaneOperator interface {
	SwapPane(src, dst string) error
	SelectPane(target string) error
	RespawnPane(target, command string) error
	RunChain(commands ...[]string) error
	DisplayMessage(target, format string) (string, error)
}

type PaneCapturer interface {
	CapturePaneLines(target string, n int) (string, error)
}
