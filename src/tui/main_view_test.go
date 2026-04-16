package tui

import (
	"strings"
	"testing"
)

func TestRenderIconPreviewBody(t *testing.T) {
	ApplyTheme("default")
	animFrame = 0

	got := renderIconPreviewBody()

	// セクションタイトルが含まれる
	if !strings.Contains(got, "STATUS ICON PREVIEW") {
		t.Error("expected STATUS ICON PREVIEW header")
	}

	// 全 4 案のラベルが含まれる
	for _, label := range []string{"Current", "Unicode", "Emoji", "NerdFont"} {
		if !strings.Contains(got, label) {
			t.Errorf("expected scheme label %q in output", label)
		}
	}

	// ステータス名ヘッダが含まれる
	for _, name := range []string{"running", "waiting", "idle", "stopped", "pending"} {
		if !strings.Contains(got, name) {
			t.Errorf("expected status name %q in output", name)
		}
	}

	// Current 案の静的 glyph が含まれる
	for _, g := range []string{"◆", "○", "■", "◇"} {
		if !strings.Contains(got, g) {
			t.Errorf("expected Current glyph %q in output", g)
		}
	}

	// Unicode⁺ 案の静的 glyph が含まれる
	for _, g := range []string{"⏸", "⏺", "⏹", "⊘"} {
		if !strings.Contains(got, g) {
			t.Errorf("expected Unicode+ glyph %q in output", g)
		}
	}

	// Emoji 案の静的 glyph が含まれる
	for _, g := range []string{"🟡", "⚪", "🔴", "🟠"} {
		if !strings.Contains(got, g) {
			t.Errorf("expected Emoji glyph %q in output", g)
		}
	}

	// frame=0 の Running spinner フレームが含まれる
	// spinner.Pulse frame[0] = "█"
	if !strings.Contains(got, "█") {
		t.Error("expected Pulse spinner frame '█' (Unicode+ Running) in output")
	}
	// spinner.Moon frame[0] = "🌑"
	if !strings.Contains(got, "🌑") {
		t.Error("expected Moon spinner frame '🌑' (Emoji Running) in output")
	}

	// NerdFont 注釈が含まれる
	if !strings.Contains(got, "NerdFont") || !strings.Contains(got, "Nerd Font") {
		t.Error("expected NerdFont note in output")
	}
}
