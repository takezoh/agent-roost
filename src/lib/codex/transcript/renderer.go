package transcript

import "github.com/takezoh/agent-roost/state"

const KindTranscript state.TabKind = "codex_transcript"

type RendererConfig struct{}

type tabRenderer struct {
	parser *Parser
}

func (r *tabRenderer) Append(data []byte) string {
	return RenderEntries(r.parser.ParseLines(data))
}

func (r *tabRenderer) Reset() {
	r.parser.Reset()
}

func init() {
	state.RegisterTabRenderer[RendererConfig](KindTranscript, func(RendererConfig) state.TabRenderer {
		return &tabRenderer{parser: NewParser()}
	})
}
