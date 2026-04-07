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

func (Claude) ResolveTitle(fsys fs.FS, projectPath string) string {
	dir := filepath.Join(".claude", "projects", ProjectDir(projectPath))
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

	return parseLastPrompt(fsys, filepath.Join(dir, jsonls[0].name))
}

// ProjectDir はプロジェクトパスを ~/.claude/projects/ のディレクトリ名に変換する。
// エンコード: / と . を - に置換
func ProjectDir(projectPath string) string {
	return strings.NewReplacer("/", "-", ".", "-").Replace(projectPath)
}

func parseLastPrompt(fsys fs.FS, path string) string {
	f, err := fsys.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	var result string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	for scanner.Scan() {
		var entry struct {
			Type       string `json:"type"`
			LastPrompt string `json:"lastPrompt"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type == "last-prompt" && entry.LastPrompt != "" {
			result = entry.LastPrompt
		}
	}
	return result
}
