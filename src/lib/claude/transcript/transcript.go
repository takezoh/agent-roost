package transcript

import (
	"bufio"
	"bytes"
	"io"
	"io/fs"
)

// ParserOptions configures a Parser.
type ParserOptions struct {
	// ShowThinking emits assistant "thinking" content blocks instead of
	// dropping them.
	ShowThinking bool

	// SubagentFS / SubagentDir locate the current session's subagent
	// files (agent-{ID}.jsonl + .meta.json). When set, Task/Agent tool
	// results are inlined with the subagent transcript expanded.
	SubagentFS  fs.FS
	SubagentDir string
}

// Parser is a stateful Claude JSONL transcript parser. It must be held
// across streaming chunks so the tool_use_id → name index and subagent
// loader cache survive between calls.
type Parser struct {
	opts         ParserOptions
	toolUseNames map[string]string
	loader       *SubagentLoader
}

func NewParser(opts ParserOptions) *Parser {
	p := &Parser{
		opts:         opts,
		toolUseNames: make(map[string]string),
	}
	if opts.SubagentFS != nil {
		dir := opts.SubagentDir
		if dir == "" {
			dir = "."
		}
		p.loader = NewSubagentLoader(opts.SubagentFS, dir, opts)
	}
	return p
}

// Reset clears accumulated state.
func (p *Parser) Reset() {
	for k := range p.toolUseNames {
		delete(p.toolUseNames, k)
	}
}

// ParseLines parses a chunk of JSONL bytes. Unknown event types and
// malformed lines are silently dropped.
func (p *Parser) ParseLines(raw []byte) []Entry {
	if len(raw) == 0 {
		return nil
	}
	var out []Entry
	for _, line := range bytes.Split(raw, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		out = append(out, p.parseLine(line)...)
	}
	return out
}

// ParseAll reads r line-by-line and returns every Entry. Used when a
// transcript needs to be inspected from scratch (e.g. ResolveMeta).
func (p *Parser) ParseAll(r io.Reader) []Entry {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var out []Entry
	for s.Scan() {
		out = append(out, p.parseLine(s.Bytes())...)
	}
	return out
}
