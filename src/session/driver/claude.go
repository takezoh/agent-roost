package driver

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/take/agent-roost/lib/claude/transcript"
)

// Claude implements the Claude Code CLI behavior.
// Prompt pattern matches only ❯ or > (to avoid false positives with bash $).
type Claude struct{}

const claudePromptPattern = `(?m)(^>|❯\s*$)`

func (Claude) Name() string          { return "claude" }
func (Claude) PromptPattern() string { return claudePromptPattern }
func (Claude) DisplayName() string   { return "claude" }

// ResolveMeta resolves session metadata from Claude Code JSONL logs.
// If sessionID is non-empty, it reads that specific file; otherwise it
// picks the most recent .jsonl in the project directory.
func (Claude) ResolveMeta(fsys fs.FS, projectPath string, sessionID string) SessionMeta {
	dir := filepath.Join(".claude", "projects", ProjectDir(projectPath))

	if sessionID != "" {
		path := filepath.Join(dir, sessionID+".jsonl")
		meta := parseSessionMeta(fsys, path)
		meta.SessionID = sessionID
		return meta
	}

	target := findNewestJSONL(fsys, dir)
	if target == "" {
		return SessionMeta{}
	}
	meta := parseSessionMeta(fsys, filepath.Join(dir, target))
	meta.SessionID = strings.TrimSuffix(target, ".jsonl")
	return meta
}

func findNewestJSONL(fsys fs.FS, dir string) string {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return ""
	}

	type file struct {
		name  string
		mtime int64
	}
	var jsonls []file
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		jsonls = append(jsonls, file{name: e.Name(), mtime: info.ModTime().UnixNano()})
	}
	if len(jsonls) == 0 {
		return ""
	}
	sort.Slice(jsonls, func(i, j int) bool {
		return jsonls[i].mtime > jsonls[j].mtime
	})
	return jsonls[0].name
}

// ProjectDir converts a project path to a ~/.claude/projects/ directory name.
// Encoding: replaces / and . with -
func ProjectDir(projectPath string) string {
	return strings.NewReplacer("/", "-", ".", "-").Replace(projectPath)
}

// TranscriptFilePath returns the JSONL transcript file path for a Claude session ID.
func TranscriptFilePath(homeDir, projectPath, sessionID string) string {
	if homeDir == "" || projectPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(homeDir, ".claude", "projects", ProjectDir(projectPath), sessionID+".jsonl")
}

// parseSessionMeta delegates to transcript.AggregateMeta for the heavy
// lifting and then projects the snapshot onto the driver SessionMeta
// shape so the rest of the codebase doesn't need to know about the
// transcript package's types.
func parseSessionMeta(fsys fs.FS, path string) SessionMeta {
	f, err := fsys.Open(path)
	if err != nil {
		return SessionMeta{}
	}
	defer f.Close()

	parser := transcript.NewParser(transcript.ParserOptions{})
	entries := parser.ParseAll(f)
	snap := transcript.AggregateMeta(entries)
	return SessionMeta{
		Title:          snap.Title,
		LastPrompt:     snap.LastPrompt,
		Subjects:       snap.Subjects,
		AgentName:      snap.Insight.AgentName,
		CurrentTool:    snap.Insight.CurrentTool,
		RecentCommands: snap.Insight.RecentCommands,
		SubagentCounts: snap.Insight.SubagentCounts,
		ErrorCount:     snap.Insight.ErrorCount,
		TouchedFiles:   snap.Insight.TouchedFiles,
	}
}
