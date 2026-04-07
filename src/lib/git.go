package lib

import (
	"os/exec"
	"strings"
)

// DetectGitBranch は指定ディレクトリの現在の git ブランチ名を返す。
// git リポジトリでない場合は空文字を返す。
func DetectGitBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
