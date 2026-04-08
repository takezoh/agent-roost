package driver

import (
	"io/fs"
	"strings"

	"github.com/take/agent-roost/lib/claude/cli"
	"github.com/take/agent-roost/lib/claude/transcript"
)

// Claude implements the Claude Code CLI behavior.
// Prompt pattern matches only ❯ or > (to avoid false positives with bash $).
type Claude struct{}

const claudePromptPattern = `(?m)(^>|❯\s*$)`

func (Claude) Name() string          { return "claude" }
func (Claude) PromptPattern() string { return claudePromptPattern }
func (Claude) DisplayName() string   { return "claude" }

// SpawnCommand returns "claude --resume <id>" when an agent session ID is
// provided so cold-boot recovery picks up the prior conversation.
func (Claude) SpawnCommand(baseCommand, agentSessionID string) string {
	return cli.ResumeCommand(baseCommand, agentSessionID)
}

// ResolveMeta resolves session metadata from a Claude Code JSONL transcript.
// transcriptPath is the absolute path Claude reports via hook events. An empty
// path returns an empty SessionMeta — roost no longer guesses the JSONL
// location from the project path because worktree-relative invocations like
// `claude --worktree` write to a different ~/.claude/projects directory than
// the session's recorded project.
func (Claude) ResolveMeta(fsys fs.FS, transcriptPath string) SessionMeta {
	if transcriptPath == "" {
		return SessionMeta{}
	}
	// fs.FS treats absolute paths as invalid; strip the leading "/" so the
	// caller can pass either an os.DirFS("/") or an os.DirFS(home) without
	// having to know which.
	return parseSessionMeta(fsys, strings.TrimPrefix(transcriptPath, "/"))
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
