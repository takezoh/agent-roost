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

	// 4 案のラベルが含まれる
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

	// Unicode 案の静的 glyph (Idle/Stopped は NerdFont と共通の PUA glyph)
	if !strings.Contains(got, "⋯") {
		t.Error("expected Unicode Waiting glyph '⋯' in output")
	}

	// Emoji 案のモダン絵文字
	for _, g := range []string{"💬", "💤", "⛔"} {
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

	// Pending blink の ⚡ が含まれる (Unicode / Emoji / NerdFont 案)
	if !strings.Contains(got, "⚡") {
		t.Error("expected Pending blink glyph '⚡' in output")
	}

	// Running spinner の frame[0] が含まれる
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

func TestStateAnim(t *testing.T) {
	// static: glyph を返す
	animFrame = 0
	s := static("◆")
	if got := s.current(); got != "◆" {
		t.Errorf("static.current() = %q, want %q", got, "◆")
	}

	// anim: frame を animFrame でサイクル
	a := anim("⚡", "∙")
	animFrame = 0
	if got := a.current(); got != "⚡" {
		t.Errorf("anim.current() at frame 0 = %q, want %q", got, "⚡")
	}
	animFrame = 1
	if got := a.current(); got != "∙" {
		t.Errorf("anim.current() at frame 1 = %q, want %q", got, "∙")
	}
	animFrame = 2
	if got := a.current(); got != "⚡" { // wraps around
		t.Errorf("anim.current() at frame 2 = %q, want %q (wrap)", got, "⚡")
	}
}
