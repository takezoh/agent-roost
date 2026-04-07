package driver

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// Claude は Claude Code CLI のふるまいを実装する。
// プロンプトパターンは ❯ または > のみ（bash の $ との誤検知を避ける）。
type Claude struct{}

const claudePromptPattern = `(?m)(^>|❯\s*$)`

func (Claude) Name() string          { return "claude" }
func (Claude) PromptPattern() string { return claudePromptPattern }
func (Claude) DisplayName() string   { return "claude" }

func (Claude) ResolveMeta(fsys fs.FS, projectPath string) SessionMeta {
	dir := filepath.Join(".claude", "projects", ProjectDir(projectPath))
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return SessionMeta{}
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
		return SessionMeta{}
	}
	sort.Slice(jsonls, func(i, j int) bool {
		return jsonls[i].mtime > jsonls[j].mtime
	})

	return parseSessionMeta(fsys, filepath.Join(dir, jsonls[0].name))
}

// ProjectDir はプロジェクトパスを ~/.claude/projects/ のディレクトリ名に変換する。
// エンコード: / と . を - に置換
func ProjectDir(projectPath string) string {
	return strings.NewReplacer("/", "-", ".", "-").Replace(projectPath)
}

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
				Content string `json:"content"`
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
			if entry.Message != nil && entry.Message.Content != "" {
				meta.LastPrompt = entry.Message.Content
			}
		}
	}
	return meta
}
