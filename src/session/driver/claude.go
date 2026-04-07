package driver

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// Claude implements the Claude Code CLI behavior.
// Prompt pattern matches only ❯ or > (to avoid false positives with bash $).
type Claude struct{}

const claudePromptPattern = `(?m)(^>|❯\s*$)`

func (Claude) Name() string          { return "claude" }
func (Claude) PromptPattern() string { return claudePromptPattern }
func (Claude) DisplayName() string   { return "claude" }

// ResolveMeta resolves session metadata from Claude Code JSONL logs.
// If source is non-empty, it reads that specific file; otherwise it picks the most recent .jsonl.
func (Claude) ResolveMeta(fsys fs.FS, projectPath string, source string) SessionMeta {
	dir := filepath.Join(".claude", "projects", ProjectDir(projectPath))

	if source != "" {
		path := filepath.Join(dir, source)
		meta := parseSessionMeta(fsys, path)
		meta.Source = source
		return meta
	}

	target := findNewestJSONL(fsys, dir)
	if target == "" {
		return SessionMeta{}
	}
	meta := parseSessionMeta(fsys, filepath.Join(dir, target))
	meta.Source = target
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

const maxSubjects = 10

func parseSessionMeta(fsys fs.FS, path string) SessionMeta {
	f, err := fsys.Open(path)
	if err != nil {
		return SessionMeta{}
	}
	defer f.Close()

	var meta SessionMeta
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	for scanner.Scan() {
		var entry struct {
			Type        string `json:"type"`
			CustomTitle string `json:"customTitle"`
			LastPrompt  string `json:"lastPrompt"`
			Message     *struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		switch entry.Type {
		case "custom-title":
			if entry.CustomTitle != "" {
				meta.Title = entry.CustomTitle
			}
		case "last-prompt":
			if entry.LastPrompt != "" {
				meta.LastPrompt = entry.LastPrompt
			}
		case "user":
			if entry.Message != nil {
				var s string
				if json.Unmarshal(entry.Message.Content, &s) == nil && s != "" {
					meta.LastPrompt = s
				}
			}
		case "assistant":
			if entry.Message != nil {
				extractSubjects(&meta, entry.Message.Content)
			}
		}
	}
	return meta
}

func extractSubjects(meta *SessionMeta, raw json.RawMessage) {
	var blocks []struct {
		Type  string `json:"type"`
		Name  string `json:"name"`
		Input struct {
			Subject string `json:"subject"`
		} `json:"input"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return
	}
	for _, b := range blocks {
		if b.Type == "tool_use" && b.Name == "TaskCreate" && b.Input.Subject != "" && len(meta.Subjects) < maxSubjects {
			meta.Subjects = append(meta.Subjects, b.Input.Subject)
		}
	}
}
