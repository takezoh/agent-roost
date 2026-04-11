package transcript

import (
	"os"

	"github.com/take/agent-roost/state"
)

type RendererConfig struct {
	SubagentDir string `json:"subagent_dir,omitempty"`
}

type tabRenderer struct {
	parser       *Parser
	subagentDir  string
	showThinking bool
}

func (r *tabRenderer) Append(data []byte) string {
	entries := r.parser.ParseLines(data)
	return RenderEntries(entries)
}

func (r *tabRenderer) Reset() {
	r.parser.Reset()
}

func (r *tabRenderer) SetShowThinking(v bool) {
	r.showThinking = v
	r.parser = newParserWithDir(r.subagentDir, v)
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
				parser:      newParserWithDir(cfg.SubagentDir, false),
				subagentDir: cfg.SubagentDir,
			}
		},
	)
}
