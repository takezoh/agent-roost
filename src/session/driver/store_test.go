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
	if sess.State != AgentStateUnset {
		t.Errorf("got State %v, want AgentStateUnset", sess.State)
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

func TestBind_Resume(t *testing.T) {
	s := NewAgentStore()

	s.Bind("win1", "agent-abc")
	s.UpdateState("agent-abc", AgentStateRunning)

	// resume: same agentID, different window
	if !s.Bind("win2", "agent-abc") {
		t.Fatal("expected true on resume bind to different window")
	}
	sess := s.Get("agent-abc")
	if sess.State != AgentStateRunning {
		t.Errorf("resume should preserve state, got %v", sess.State)
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

func TestUpdateState(t *testing.T) {
	s := NewAgentStore()

	// update nonexistent returns false
	if s.UpdateState("nonexistent", AgentStateIdle) {
		t.Error("expected false for nonexistent session")
	}

	s.Bind("win1", "agent-abc")

	if !s.UpdateState("agent-abc", AgentStateRunning) {
		t.Error("expected true on state change")
	}
	if s.Get("agent-abc").State != AgentStateRunning {
		t.Error("state not updated")
	}

	// same value returns false
	if s.UpdateState("agent-abc", AgentStateRunning) {
		t.Error("expected false when state unchanged")
	}

	if !s.UpdateState("agent-abc", AgentStateStopped) {
		t.Error("expected true on state change")
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

func TestAgentState_String(t *testing.T) {
	tests := []struct {
		state AgentState
		want  string
	}{
		{AgentStateUnset, "unset"},
		{AgentStateIdle, "idle"},
		{AgentStateRunning, "running"},
		{AgentStateWaiting, "waiting"},
		{AgentStatePending, "pending"},
		{AgentStateStopped, "stopped"},
		{AgentState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("AgentState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}
