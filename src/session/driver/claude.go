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

// DriverState keys produced/consumed by the Claude driver. core never
// references these constants — they live entirely inside this file so adding
// a Claude-specific field is a single-file change.
const (
	claudeKeySessionID      = "session_id"
	claudeKeyWorkingDir     = "working_dir"
	claudeKeyTranscriptPath = "transcript_path"
)

func (Claude) Name() string          { return "claude" }
func (Claude) PromptPattern() string { return claudePromptPattern }
func (Claude) DisplayName() string   { return "claude" }

// IdentityKey returns the DriverState key that uniquely identifies a Claude
// agent process — the Claude session ID, which is what AgentStore.Bind uses.
func (Claude) IdentityKey() string { return claudeKeySessionID }

// WorkingDir returns the agent's reported cwd, used for git branch detection
// when the agent runs in a worktree.
func (Claude) WorkingDir(sc SessionContext) string {
	return sc.DriverState[claudeKeyWorkingDir]
}

// SpawnCommand returns "claude --resume <id>" when an agent session ID is
// known so cold-boot recovery picks up the prior conversation.
func (Claude) SpawnCommand(baseCommand string, sc SessionContext) string {
	return cli.ResumeCommand(baseCommand, sc.DriverState[claudeKeySessionID])
}

// TranscriptFilePath returns the absolute JSONL transcript path for the
// current session. Priority:
//  1. The path the agent itself reported (canonical, handles --worktree)
//  2. A computed path based on the agent's working dir + session ID
//  3. A computed path based on sc.Project + session ID (pre-hook fallback)
func (Claude) TranscriptFilePath(home string, sc SessionContext) string {
	if reported := sc.DriverState[claudeKeyTranscriptPath]; reported != "" {
		return reported
	}
	sid := sc.DriverState[claudeKeySessionID]
	if home == "" || sid == "" {
		return ""
	}
	workdir := sc.DriverState[claudeKeyWorkingDir]
	if workdir == "" {
		workdir = sc.Project
	}
	if workdir == "" {
		return ""
	}
	return filepath.Join(home, ".claude", "projects", projectDir(workdir), sid+".jsonl")
}

// ResolveMeta reads session metadata from Claude's JSONL transcript file.
// Missing files yield an empty SessionMeta silently.
func (Claude) ResolveMeta(fsys fs.FS, home string, sc SessionContext) SessionMeta {
	path := Claude{}.TranscriptFilePath(home, sc)
	if path == "" {
		return SessionMeta{}
	}
	// fs.FS treats absolute paths as invalid; strip the leading "/" so the
	// caller can pass an os.DirFS("/") and have absolute paths work directly.
	return parseSessionMeta(fsys, strings.TrimPrefix(path, "/"))
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
