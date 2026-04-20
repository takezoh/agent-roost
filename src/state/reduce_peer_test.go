package state

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/takezoh/agent-roost/features"
)

// peerTestState builds a minimal State with two sessions/frames for peer tests.
// frameA and frameB are both in "/workspace" (same workspace root, different projects).
// statusA/statusB control the driver's reported status.
func peerTestState(statusA, statusB Status) (State, FrameID, FrameID) { //nolint:unparam
	s := New()
	s.Now = time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	s.Features = features.Set{features.Peers: true}

	idA := SessionID("sess-aaa")
	idB := SessionID("sess-bbb")
	frameA := FrameID("frame-aaaa1234")
	frameB := FrameID("frame-bbbb5678")

	// Both projects share the same first path segment "/workspace",
	// so workspaceOf("/workspace/proj-a") == workspaceOf("/workspace/proj-b") == "/workspace".
	s.Sessions[idA] = Session{
		ID:      idA,
		Project: "/workspace/proj-a",
		Command: "stub",
		Driver:  stubDriverState{status: statusA},
		Frames: []SessionFrame{{
			ID:      frameA,
			Project: "/workspace/proj-a",
			Command: "stub",
			Driver:  stubDriverState{status: statusA},
		}},
	}
	s.Sessions[idB] = Session{
		ID:      idB,
		Project: "/workspace/proj-b",
		Command: "stub",
		Driver:  stubDriverState{status: statusB},
		Frames: []SessionFrame{{
			ID:      frameB,
			Project: "/workspace/proj-b",
			Command: "stub",
			Driver:  stubDriverState{status: statusB},
		}},
	}

	return s, frameA, frameB
}

// TestReducePeerSend_IdleInjects: sending to an idle frame produces EffInjectPrompt,
// not an inbox queue entry.
func TestReducePeerSend_IdleInjects(t *testing.T) {
	s, frameA, frameB := peerTestState(StatusIdle, StatusIdle)

	params := PeerSendParams{
		ToFrameID:   string(frameB),
		FromFrameID: string(frameA),
		Text:        "hello from A",
	}
	payload, _ := json.Marshal(params)

	_, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: EventPeerSend,
		Payload: json.RawMessage(payload),
	})

	mustOK(t, effs)

	inject, ok := findEff[EffInjectPrompt](effs)
	if !ok {
		t.Fatal("expected EffInjectPrompt for idle target")
	}
	if inject.FrameID != frameB {
		t.Errorf("inject.FrameID = %q, want %q", inject.FrameID, frameB)
	}
	if inject.Text == "" {
		t.Error("inject.Text should not be empty")
	}
}

// TestReducePeerSend_BusyQueues: sending to a running frame queues in PeerInbox.
func TestReducePeerSend_BusyQueues(t *testing.T) {
	s, frameA, frameB := peerTestState(StatusIdle, StatusRunning)

	params := PeerSendParams{
		ToFrameID:   string(frameB),
		FromFrameID: string(frameA),
		Text:        "hello from A while B is running",
	}
	payload, _ := json.Marshal(params)

	next, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: EventPeerSend,
		Payload: json.RawMessage(payload),
	})
	mustOK(t, effs)

	// Must NOT emit EffInjectPrompt.
	if _, ok := findEff[EffInjectPrompt](effs); ok {
		t.Error("must not emit EffInjectPrompt for busy target")
	}

	// Must queue in PeerInbox of frameB.
	_, toSess, toIdx, ok := findFrame(next, frameB)
	if !ok {
		t.Fatal("frameB not found in next state")
	}
	frame := toSess.Frames[toIdx]
	if len(frame.PeerInbox) != 1 {
		t.Fatalf("inbox len = %d, want 1", len(frame.PeerInbox))
	}
	if frame.PeerInbox[0].Text != "hello from A while B is running" {
		t.Errorf("inbox text = %q", frame.PeerInbox[0].Text)
	}
	if frame.PeerInbox[0].Delivered {
		t.Error("message should not be marked delivered when queued")
	}
}

// TestReducePeerSend_UnknownTarget: non-existent ToFrameID returns errResp.
func TestReducePeerSend_UnknownTarget(t *testing.T) {
	s, frameA, _ := peerTestState(StatusIdle, StatusIdle)

	params := PeerSendParams{
		ToFrameID:   "ghost-frame",
		FromFrameID: string(frameA),
		Text:        "hello",
	}
	payload, _ := json.Marshal(params)

	_, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: EventPeerSend,
		Payload: json.RawMessage(payload),
	})

	if _, ok := findEff[EffSendError](effs); !ok {
		t.Error("expected EffSendError for unknown target frame")
	}
}

// TestReducePeerList_WorkspaceScope: only frames in the same workspace are returned.
func TestReducePeerList_WorkspaceScope(t *testing.T) {
	s, frameA, frameB := peerTestState(StatusIdle, StatusIdle)

	// Add a third session in a different workspace.
	idC := SessionID("sess-ccc")
	frameC := FrameID("frame-cccc9999")
	s.Sessions[idC] = Session{
		ID:      idC,
		Project: "/other-workspace/proj-c",
		Command: "stub",
		Driver:  stubDriverState{status: StatusIdle},
		Frames: []SessionFrame{{
			ID:      frameC,
			Project: "/other-workspace/proj-c",
			Command: "stub",
			Driver:  stubDriverState{status: StatusIdle},
		}},
	}

	params := PeerListParams{
		Scope:       "workspace",
		FromFrameID: string(frameA),
	}
	payload, _ := json.Marshal(params)

	_, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: EventPeerList,
		Payload: json.RawMessage(payload),
	})
	mustOK(t, effs)

	resp, ok := findEff[EffSendResponse](effs)
	if !ok {
		t.Fatal("expected EffSendResponse")
	}
	reply, ok := resp.Body.(PeerListReply)
	if !ok {
		t.Fatalf("body type = %T, want PeerListReply", resp.Body)
	}

	// frameA is the caller so it must be excluded; frameB is same workspace; frameC is not.
	for _, p := range reply.Peers {
		if p.FrameID == string(frameA) {
			t.Error("caller frame should not appear in peer list")
		}
		if p.FrameID == string(frameC) {
			t.Error("frame in different workspace should not appear with workspace scope")
		}
	}

	found := false
	for _, p := range reply.Peers {
		if p.FrameID == string(frameB) {
			found = true
		}
	}
	if !found {
		t.Error("frameB in same workspace should appear in peer list")
	}
}

// TestDrainPeerInbox: after a status transition from running→idle, queued
// messages are drained and EffInjectPrompt is emitted.
func TestDrainPeerInbox(t *testing.T) {
	// Build a state where frameB has a queued message.
	s, frameA, frameB := peerTestState(StatusIdle, StatusRunning)

	// Send a message to busy frameB — it should be queued.
	params := PeerSendParams{
		ToFrameID:   string(frameB),
		FromFrameID: string(frameA),
		Text:        "queued message",
	}
	payload, _ := json.Marshal(params)
	s, _ = Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r1", Event: EventPeerSend,
		Payload: json.RawMessage(payload),
	})

	// Verify message is queued.
	_, toSess, toIdx, ok := findFrame(s, frameB)
	if !ok {
		t.Fatal("frameB not found")
	}
	if len(toSess.Frames[toIdx].PeerInbox) != 1 {
		t.Fatalf("inbox len = %d, want 1 after send", len(toSess.Frames[toIdx].PeerInbox))
	}

	// Now simulate a status transition running→idle via drainPeerInbox directly.
	sessID, _, _, _ := findFrame(s, frameB)
	next, effs := drainPeerInbox(s, sessID, frameB, StatusRunning, StatusIdle)

	if _, ok := findEff[EffInjectPrompt](effs); !ok {
		t.Fatal("expected EffInjectPrompt after drain")
	}

	// Inbox should be empty in new state.
	_, newSess, newIdx, ok2 := findFrame(next, frameB)
	if !ok2 {
		t.Fatal("frameB not found in next state")
	}
	if len(newSess.Frames[newIdx].PeerInbox) != 0 {
		t.Errorf("inbox len = %d, want 0 after drain", len(newSess.Frames[newIdx].PeerInbox))
	}
}

// TestReducePeerSetSummary: updating summary is reflected in state.
func TestReducePeerSetSummary(t *testing.T) {
	s, frameA, _ := peerTestState(StatusIdle, StatusIdle)

	params := PeerSetSummaryParams{
		Summary:     "working on refactor",
		FromFrameID: string(frameA),
	}
	payload, _ := json.Marshal(params)

	next, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: EventPeerSetSummary,
		Payload: json.RawMessage(payload),
	})
	mustOK(t, effs)

	_, sess, idx, ok := findFrame(next, frameA)
	if !ok {
		t.Fatal("frameA not found")
	}
	if sess.Frames[idx].PeerSummary != "working on refactor" {
		t.Errorf("PeerSummary = %q, want %q", sess.Frames[idx].PeerSummary, "working on refactor")
	}

	if _, ok := findEff[EffBroadcastSessionsChanged](effs); !ok {
		t.Error("expected EffBroadcastSessionsChanged")
	}
}

// TestReducePeerDrainInbox_Empty: draining an empty inbox returns nil messages and no state change.
func TestReducePeerDrainInbox_Empty(t *testing.T) {
	s, frameA, _ := peerTestState(StatusIdle, StatusIdle)

	params := PeerDrainInboxParams{FromFrameID: string(frameA)}
	payload, _ := json.Marshal(params)

	next, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: EventPeerDrainInbox,
		Payload: json.RawMessage(payload),
	})
	mustOK(t, effs)

	resp, ok := findEff[EffSendResponse](effs)
	if !ok {
		t.Fatal("expected EffSendResponse")
	}
	reply, ok := resp.Body.(PeerDrainInboxReply)
	if !ok {
		t.Fatalf("body type = %T, want PeerDrainInboxReply", resp.Body)
	}
	if len(reply.Messages) != 0 {
		t.Errorf("messages len = %d, want 0", len(reply.Messages))
	}

	// State should be unchanged (no EffBroadcastSessionsChanged).
	if _, ok := findEff[EffBroadcastSessionsChanged](effs); ok {
		t.Error("must not emit EffBroadcastSessionsChanged for empty inbox drain")
	}
	_ = next
}

// TestReducePeerDrainInbox_Drains: draining a non-empty inbox returns messages and clears the inbox.
func TestReducePeerDrainInbox_Drains(t *testing.T) {
	s, frameA, frameB := peerTestState(StatusIdle, StatusRunning)

	// Queue a message into frameB's inbox by sending while busy.
	sendParams := PeerSendParams{
		ToFrameID:   string(frameB),
		FromFrameID: string(frameA),
		Text:        "hello queued",
	}
	payload, _ := json.Marshal(sendParams)
	s, _ = Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r1", Event: EventPeerSend,
		Payload: json.RawMessage(payload),
	})

	// Verify the message is queued.
	_, sess, idx, ok := findFrame(s, frameB)
	if !ok {
		t.Fatal("frameB not found")
	}
	if len(sess.Frames[idx].PeerInbox) != 1 {
		t.Fatalf("inbox len = %d, want 1 before drain", len(sess.Frames[idx].PeerInbox))
	}

	// Drain the inbox.
	drainParams := PeerDrainInboxParams{FromFrameID: string(frameB)}
	drainPayload, _ := json.Marshal(drainParams)
	next, effs := Reduce(s, EvEvent{
		ConnID: 2, ReqID: "r2", Event: EventPeerDrainInbox,
		Payload: json.RawMessage(drainPayload),
	})
	mustOK(t, effs)

	resp, ok := findEff[EffSendResponse](effs)
	if !ok {
		t.Fatal("expected EffSendResponse")
	}
	reply, ok := resp.Body.(PeerDrainInboxReply)
	if !ok {
		t.Fatalf("body type = %T, want PeerDrainInboxReply", resp.Body)
	}
	if len(reply.Messages) != 1 {
		t.Fatalf("messages len = %d, want 1", len(reply.Messages))
	}
	if reply.Messages[0].Text != "hello queued" {
		t.Errorf("message text = %q, want %q", reply.Messages[0].Text, "hello queued")
	}

	// Inbox must be cleared in next state.
	_, nextSess, nextIdx, ok2 := findFrame(next, frameB)
	if !ok2 {
		t.Fatal("frameB not found in next state")
	}
	if len(nextSess.Frames[nextIdx].PeerInbox) != 0 {
		t.Errorf("inbox len = %d, want 0 after drain", len(nextSess.Frames[nextIdx].PeerInbox))
	}

	// Must emit EffBroadcastSessionsChanged.
	if _, ok := findEff[EffBroadcastSessionsChanged](effs); !ok {
		t.Error("expected EffBroadcastSessionsChanged after non-empty drain")
	}
}

// TestReducePeerList_AllScope: all scope returns frames from all workspaces.
func TestReducePeerList_AllScope(t *testing.T) {
	s, frameA, _ := peerTestState(StatusIdle, StatusIdle)

	// Add a third session in a different workspace.
	idC := SessionID("sess-ccc2")
	frameC := FrameID("frame-cccc0000")
	s.Sessions[idC] = Session{
		ID:      idC,
		Project: "/other-workspace/proj-c",
		Command: "stub",
		Driver:  stubDriverState{status: StatusIdle},
		Frames: []SessionFrame{{
			ID:      frameC,
			Project: "/other-workspace/proj-c",
			Command: "stub",
			Driver:  stubDriverState{status: StatusIdle},
		}},
	}

	params := PeerListParams{
		Scope:       "all",
		FromFrameID: string(frameA),
	}
	payload, _ := json.Marshal(params)

	_, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: EventPeerList,
		Payload: json.RawMessage(payload),
	})
	mustOK(t, effs)

	resp, ok := findEff[EffSendResponse](effs)
	if !ok {
		t.Fatal("expected EffSendResponse")
	}
	reply := resp.Body.(PeerListReply)

	found := false
	for _, p := range reply.Peers {
		if p.FrameID == string(frameC) {
			found = true
		}
	}
	if !found {
		t.Error("frameC should appear with scope=all")
	}
}
