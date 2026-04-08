package transcript

import (
	"encoding/json"
	"io/fs"
	"path"
)

// SubagentLoader resolves subagent agent-{ID}.jsonl files (and their
// .meta.json companions) under a given fs.FS root and parses them into
// Entry slices. Results are cached per agentID so repeated lookups are
// cheap and recursive subagents do not loop.
type SubagentLoader struct {
	fsys  fs.FS
	root  string
	opts  ParserOptions
	cache map[string][]Entry
}

// NewSubagentLoader builds a loader rooted at root inside fsys. opts
// is propagated to inner Parsers so flags like ShowThinking carry into
// nested transcripts.
func NewSubagentLoader(fsys fs.FS, root string, opts ParserOptions) *SubagentLoader {
	return &SubagentLoader{
		fsys:  fsys,
		root:  root,
		opts:  opts,
		cache: make(map[string][]Entry),
	}
}

// Load returns the Entry slice for the given agentID, with each entry's
// Depth shifted by baseDepth so the slice can be inlined under a parent
// transcript at any nesting level. Returns nil if the loader is not
// configured or the files cannot be opened.
func (l *SubagentLoader) Load(agentID string, baseDepth int) []Entry {
	if l == nil || l.fsys == nil || agentID == "" {
		return nil
	}
	cached, ok := l.cache[agentID]
	if !ok {
		cached = l.loadFromDisk(agentID)
		l.cache[agentID] = cached
	}
	if len(cached) == 0 {
		return nil
	}
	out := make([]Entry, len(cached))
	for i, e := range cached {
		e.Depth += baseDepth
		out[i] = e
	}
	return out
}

// loadFromDisk reads agent-{ID}.jsonl + .meta.json once. Depth on
// returned entries is relative to baseDepth=0.
func (l *SubagentLoader) loadFromDisk(agentID string) []Entry {
	jsonlPath := path.Join(l.root, "agent-"+agentID+".jsonl")
	metaPath := path.Join(l.root, "agent-"+agentID+".meta.json")

	meta := readSubagentMeta(l.fsys, metaPath)

	f, err := l.fsys.Open(jsonlPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	inner := NewParser(l.opts)
	inner.loader = l // share loader so transitive subagents resolve & cache
	body := inner.ParseAll(f)

	out := make([]Entry, 0, len(body)+2)
	out = append(out, Entry{
		Kind:  KindSubagentBegin,
		Depth: 1,
		Text:  formatSubagentLabel(meta, agentID),
	})
	for _, e := range body {
		e.Depth += 1
		out = append(out, e)
	}
	out = append(out, Entry{
		Kind:  KindSubagentEnd,
		Depth: 1,
		Text:  meta.AgentType,
	})
	return out
}

type subagentMeta struct {
	AgentType   string `json:"agentType"`
	Description string `json:"description"`
}

func readSubagentMeta(fsys fs.FS, path string) subagentMeta {
	var m subagentMeta
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return m
	}
	_ = json.Unmarshal(data, &m)
	return m
}

func formatSubagentLabel(m subagentMeta, agentID string) string {
	switch {
	case m.AgentType != "" && m.Description != "":
		return m.AgentType + ": " + m.Description
	case m.AgentType != "":
		return m.AgentType
	case m.Description != "":
		return m.Description
	}
	return agentID
}
