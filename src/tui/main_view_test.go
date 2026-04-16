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
	for _, title := range []string{"STATUS ICON PREVIEW", "SPINNER FRAMES"} {
		if !strings.Contains(got, title) {
			t.Errorf("expected section title %q in output", title)
		}
	}

	// 4 案のラベルが含まれる (Current はベースラインとして非表示)
	for _, label := range []string{"Unicode", "Emoji", "NerdFont", "Spinner"} {
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

	// Unicode 案の静的 glyph (Waiting〜Pending)
	for _, g := range []string{"⏸", "⏺", "⏹", "⊘"} {
		if !strings.Contains(got, g) {
			t.Errorf("expected Unicode glyph %q in output", g)
		}
	}

	// Emoji 案の静的 glyph
	for _, g := range []string{"🟡", "⚪", "🔴", "🟠"} {
		if !strings.Contains(got, g) {
			t.Errorf("expected Emoji glyph %q in output", g)
		}
	}

	// Spinner 案は現行の幾何アイコンを維持
	for _, g := range []string{"◆", "○", "■", "◇"} {
		if !strings.Contains(got, g) {
			t.Errorf("expected Spinner scheme glyph %q in output", g)
		}
	}

	// frame=0 の spinner フレームが SPINNER FRAMES に含まれる
	// Pulse frame[0] = "█"
	if !strings.Contains(got, "█") {
		t.Error("expected Pulse frame '█' (Unicode Running) in output")
	}
	// Moon frame[0] = "🌑"
	if !strings.Contains(got, "🌑") {
		t.Error("expected Moon frame '🌑' (Emoji Running) in output")
	}
	// Hamburger frame[0] = "☱"
	if !strings.Contains(got, "☱") {
		t.Error("expected Hamburger frame '☱' (Spinner Running) in output")
	}

	// NerdFont 注釈が含まれる
	if !strings.Contains(got, "Nerd Font") {
		t.Error("expected NerdFont note in output")
	}
}
