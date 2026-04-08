package driver

import (
	"io/fs"
	"path/filepath"
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

// TranscriptFilePath returns the JSONL transcript file Claude writes for the
// given runtime context. Claude derives its project directory name from the
// process working directory (replacing / and . with -), so for worktree-style
// invocations the file lives under the worktree path, not the session's
// recorded project. Used as a fallback when the agent hasn't yet reported its
// transcript path via a hook event.
func (Claude) TranscriptFilePath(home, workingDir, agentSessionID string) string {
	if home == "" || workingDir == "" || agentSessionID == "" {
		return ""
	}
	return filepath.Join(home, ".claude", "projects", projectDir(workingDir), agentSessionID+".jsonl")
}

// ResolveMeta reads session metadata from Claude's JSONL transcript at the
// given absolute path. Missing files yield an empty SessionMeta silently.
func (Claude) ResolveMeta(fsys fs.FS, transcriptPath string) SessionMeta {
	if transcriptPath == "" {
		return SessionMeta{}
	}
	// fs.FS treats absolute paths as invalid; strip the leading "/" so the
	// caller can pass an os.DirFS("/") and have absolute paths work directly.
	return parseSessionMeta(fsys, strings.TrimPrefix(transcriptPath, "/"))
}

// projectDir mirrors Claude Code's encoding of working dir → ~/.claude/projects/
// dir name: replace / and . with -.
func projectDir(p string) string {
	return strings.NewReplacer("/", "-", ".", "-").Replace(p)
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
