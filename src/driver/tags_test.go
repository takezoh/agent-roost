package driver

import "testing"

func TestShellCommandTagBrandColors(t *testing.T) {
	tests := []struct {
		name   string
		wantBg string
		wantFg string
	}{
		{"bash", bashTagBg, commandTagFg},
		{"zsh", zshTagBg, commandTagFg},
		{"fish", fishTagBg, commandTagFg},
		{"powershell", powershellTagBg, powershellTagFg},
		{"pwsh", powershellTagBg, powershellTagFg},
		{"nu", nushellTagBg, commandTagFg},
		{"nushell", nushellTagBg, commandTagFg},
		{"unknown-shell", commandTagBg, commandTagFg},
		{"/usr/bin/bash", bashTagBg, commandTagFg},
		{"Bash", bashTagBg, commandTagFg},
		{"ZSH", zshTagBg, commandTagFg},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tag := ShellCommandTag(tt.name)
			if tag.Text != tt.name {
				t.Errorf("Text = %q, want %q", tag.Text, tt.name)
			}
			if tag.Background != tt.wantBg {
				t.Errorf("Background = %q, want %q", tag.Background, tt.wantBg)
			}
			if tag.Foreground != tt.wantFg {
				t.Errorf("Foreground = %q, want %q", tag.Foreground, tt.wantFg)
			}
		})
	}
}
