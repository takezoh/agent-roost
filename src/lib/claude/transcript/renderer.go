package transcript

import (
	"os"

	"github.com/take/agent-roost/state"
)

type RendererConfig struct {
	SubagentDir  string `json:"subagent_dir,omitempty"`
	ShowThinking bool   `json:"show_thinking,omitempty"`
}

type tabRenderer struct {
	parser      *Parser
	subagentDir string
}

func (r *tabRenderer) Append(data []byte) string {
	entries := r.parser.ParseLines(data)
	return RenderEntries(entries)
}

func (r *tabRenderer) Reset() {
	r.parser.Reset()
}

func newParserWithDir(dir string, showThinking bool) *Parser {
	opts := ParserOptions{
		ShowThinking: showThinking,
		SubagentDir:  dir,
	}
	if dir != "" {
		opts.SubagentFS = os.DirFS(dir)
		opts.SubagentDir = "."
	}
	return NewParser(opts)
}

func init() {
	state.RegisterTabRenderer[RendererConfig](
		state.TabKindTranscript,
		func(cfg RendererConfig) state.TabRenderer {
			return &tabRenderer{
				parser:      newParserWithDir(cfg.SubagentDir, cfg.ShowThinking),
				subagentDir: cfg.SubagentDir,
			}
		},
	)
}
