// Package tools is the user-facing palette tool registry. Each tool
// describes a high-level operation (create session, stop session,
// detach, ...) the palette can drive. Tools are decoupled from the
// daemon by going through proto.Client — the same interface the TUI
// processes use to talk to the daemon.
package tools

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/sahilm/fuzzy"
	"github.com/takezoh/agent-roost/proto"
)

// Param describes one input the palette must collect before running
// a tool. Options is called at runtime to populate the picker.
type Param struct {
	Name    string
	Options func(ctx *ToolContext) []string
}

// Tool is one user-facing palette command.
type Tool struct {
	Name        string
	Description string
	Hidden      bool // Hidden tools are excluded from All() and Match() but reachable via Get()
	Params      []Param
	Run         func(ctx *ToolContext, args map[string]string) (*ToolInvocation, error)
}

// ToolInvocation tells the palette to immediately start another tool
// in the same popup process after the current Run returns. Used for
// in-popup tool chains (e.g. create-project → new-session) since
// tmux disallows nested display-popups.
type ToolInvocation struct {
	Name string
	Args map[string]string
}

// ToolContext is the bag of dependencies handed to every Tool.Run
// call. Client is the daemon connection; Config carries static
// palette config (commands, projects).
type ToolContext struct {
	Client       *proto.Client
	Config       ToolConfig
	Args         map[string]string
	IsGitProject func(path string) bool // nil-safe; injected by main
}

type ToolConfig struct {
	DefaultCommand string
	Commands       []string
	PushCommands   []string
	Projects       []string
	ProjectRoots   []string
}

// Registry holds the tools available to the palette.
type Registry struct {
	tools  []Tool
	byName map[string]*Tool
}

func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]*Tool)}
}

func (r *Registry) Register(t Tool) {
	r.tools = append(r.tools, t)
	r.byName[t.Name] = &r.tools[len(r.tools)-1]
}

func (r *Registry) Get(name string) *Tool { return r.byName[name] }

func (r *Registry) All() []Tool {
	var visible []Tool
	for _, t := range r.tools {
		if !t.Hidden {
			visible = append(visible, t)
		}
	}
	return visible
}

// MatchedTool pairs a Tool with the rune-offsets within Tool.Name that
// matched the query, for use by callers that want to highlight matches.
type MatchedTool struct {
	Tool    Tool
	Indexes []int // rune offsets in Tool.Name; nil when query is empty
}

// Match returns visible tools whose Name fuzzy-matches query, ordered by
// descending score. Empty query (or all-whitespace) returns all visible tools
// in registration order with no indexes. Space-separated tokens are matched
// independently — all tokens must match for a tool to be included.
func (r *Registry) Match(query string) []MatchedTool {
	var visible []Tool
	for _, t := range r.tools {
		if !t.Hidden {
			visible = append(visible, t)
		}
	}
	tokens := strings.Fields(query)
	if len(tokens) == 0 {
		out := make([]MatchedTool, len(visible))
		for i, t := range visible {
			out[i] = MatchedTool{Tool: t}
		}
		return out
	}
	type scored struct {
		tool  Tool
		score int
		idx   []int
	}
	var results []scored
	for _, t := range visible {
		totalScore := 0
		var merged []int
		ok := true
		for _, tok := range tokens {
			res := fuzzy.Find(tok, []string{t.Name})
			if len(res) == 0 {
				ok = false
				break
			}
			totalScore += res[0].Score
			merged = append(merged, res[0].MatchedIndexes...)
		}
		if ok {
			results = append(results, scored{tool: t, score: totalScore, idx: dedupeInts(merged)})
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].score > results[j].score })
	out := make([]MatchedTool, len(results))
	for i, s := range results {
		out[i] = MatchedTool{Tool: s.tool, Indexes: s.idx}
	}
	return out
}

func dedupeInts(s []int) []int {
	if len(s) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(s))
	out := s[:0]
	for _, v := range s {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}

// ProjectDisplayName returns the human-readable label for a project
// path (currently just basename).
func ProjectDisplayName(path string) string {
	return filepath.Base(path)
}
