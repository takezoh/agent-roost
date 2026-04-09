package driver

import "testing"

func TestBind(t *testing.T) {
	s := NewAgentStore()

	if !s.Bind("win1", "agent-abc") {
		t.Fatal("expected true on first Bind")
	}
	sess := s.GetByWindow("win1")
	if sess == nil {
		t.Fatal("expected session after Bind")
	}
	if sess.ID != "agent-abc" {
		t.Errorf("got ID %q, want %q", sess.ID, "agent-abc")
	}

	// rebind same window to same agent returns false
	if s.Bind("win1", "agent-abc") {
		t.Fatal("expected false on same rebind")
	}

	// rebind same window to different agent
	if !s.Bind("win1", "agent-xyz") {
		t.Fatal("expected true on rebind to different agent")
	}
	sess = s.GetByWindow("win1")
	if sess == nil {
		t.Fatal("expected session after rebind")
	}
	if sess.ID != "agent-xyz" {
		t.Errorf("got ID %q, want %q", sess.ID, "agent-xyz")
	}

	// original session still exists
	orig := s.Get("agent-abc")
	if orig == nil {
		t.Fatal("original session should still exist after rebind")
	}
}

func TestBind_ResumePreservesMetadata(t *testing.T) {
	s := NewAgentStore()

	s.Bind("win1", "agent-abc")
	s.UpdateStatusLine("agent-abc", "status: ok")

	// resume: same agentID, different window
	if !s.Bind("win2", "agent-abc") {
		t.Fatal("expected true on resume bind to different window")
	}
	sess := s.Get("agent-abc")
	if sess.StatusLine != "status: ok" {
		t.Errorf("resume should preserve metadata, got StatusLine %q", sess.StatusLine)
	}
	sess2 := s.GetByWindow("win2")
	if sess2 != sess {
		t.Error("expected same session pointer on resume")
	}
}

func TestUnbind(t *testing.T) {
	s := NewAgentStore()

	s.Bind("win1", "agent-abc")
	s.Unbind("win1")

	if sess := s.GetByWindow("win1"); sess != nil {
		t.Error("expected nil after Unbind")
	}

	// session itself remains
	if sess := s.Get("agent-abc"); sess == nil {
		t.Error("session should still exist after Unbind")
	}

	// unbind nonexistent is safe
	s.Unbind("win-nonexistent")
}

func TestGet(t *testing.T) {
	s := NewAgentStore()

	if sess := s.Get("nonexistent"); sess != nil {
		t.Error("expected nil for nonexistent ID")
	}

	s.Bind("win1", "agent-abc")
	sess := s.Get("agent-abc")
	if sess == nil {
		t.Fatal("expected session")
	}
	if sess.ID != "agent-abc" {
		t.Errorf("got ID %q, want %q", sess.ID, "agent-abc")
	}
}

func TestUpdateStatusLine(t *testing.T) {
	s := NewAgentStore()

	if s.UpdateStatusLine("nonexistent", "line") {
		t.Error("expected false for nonexistent session")
	}

	s.Bind("win1", "agent-abc")

	if !s.UpdateStatusLine("agent-abc", "status: ok") {
		t.Error("expected true on status change")
	}
	if s.Get("agent-abc").StatusLine != "status: ok" {
		t.Error("status line not updated")
	}

	// same value returns false
	if s.UpdateStatusLine("agent-abc", "status: ok") {
		t.Error("expected false when status unchanged")
	}

	if !s.UpdateStatusLine("agent-abc", "status: error") {
		t.Error("expected true on status change")
	}
}

func TestUpdateMeta(t *testing.T) {
	s := NewAgentStore()

	if s.UpdateMeta("nonexistent", SessionMeta{Title: "t"}) {
		t.Error("expected false for nonexistent session")
	}

	s.Bind("win1", "agent-abc")

	// full update
	changed := s.UpdateMeta("agent-abc", SessionMeta{
		Title:      "My Task",
		LastPrompt: "do something",
		Subjects:   []string{"feat", "bug"},
	})
	if !changed {
		t.Error("expected true on meta update")
	}
	sess := s.Get("agent-abc")
	if sess.Title != "My Task" {
		t.Errorf("title = %q, want %q", sess.Title, "My Task")
	}
	if sess.LastPrompt != "do something" {
		t.Errorf("lastPrompt = %q, want %q", sess.LastPrompt, "do something")
	}
	if len(sess.Subjects) != 2 || sess.Subjects[0] != "feat" {
		t.Errorf("subjects = %v, want [feat bug]", sess.Subjects)
	}

	// same values returns false
	if s.UpdateMeta("agent-abc", SessionMeta{
		Title:      "My Task",
		LastPrompt: "do something",
		Subjects:   []string{"feat", "bug"},
	}) {
		t.Error("expected false when meta unchanged")
	}

	// partial update: only title changes
	if !s.UpdateMeta("agent-abc", SessionMeta{Title: "New Title"}) {
		t.Error("expected true on partial meta update")
	}
	if sess.Title != "New Title" {
		t.Errorf("title = %q, want %q", sess.Title, "New Title")
	}
	// other fields unchanged
	if sess.LastPrompt != "do something" {
		t.Errorf("lastPrompt should be unchanged, got %q", sess.LastPrompt)
	}

	// empty fields don't overwrite
	if s.UpdateMeta("agent-abc", SessionMeta{}) {
		t.Error("expected false when all meta fields empty")
	}
}

func TestRestoreFromBindings(t *testing.T) {
	s := NewAgentStore()
	s.RestoreFromBindings(map[string]string{
		"@1": "agent-1",
		"@2": "agent-2",
		"@3": "",        // skipped
		"":   "agent-x", // skipped
	})

	if s.IDByWindow("@1") != "agent-1" {
		t.Errorf("@1 binding lost")
	}
	if s.IDByWindow("@2") != "agent-2" {
		t.Errorf("@2 binding lost")
	}
	if s.IDByWindow("@3") != "" {
		t.Errorf("empty agent ID should be skipped")
	}
	if s.Get("agent-1") == nil {
		t.Errorf("expected agent-1 session to be created")
	}
}
