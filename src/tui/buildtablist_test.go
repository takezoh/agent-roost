package tui

import (
	"testing"

	"github.com/take/agent-roost/proto"
	"github.com/take/agent-roost/state"
)

func TestBuildTabList_DriverProvidedTabsThenInfoThenLog(t *testing.T) {
	current := &proto.SessionInfo{
		ID: "s1",
		View: state.View{
			LogTabs: []state.LogTab{
				{Label: "TRANSCRIPT", Path: "/tmp/x.jsonl", Kind: state.TabKindTranscript},
				{Label: "EVENTS", Path: "/tmp/x.log", Kind: state.TabKindText},
			},
			InfoExtras: []state.InfoLine{{Label: "k", Value: "v"}},
		},
	}
	tabs := buildTabList(map[string]*tabState{}, current, "/var/log/roost.log")

	wantLabels := []string{"TRANSCRIPT", "EVENTS", "INFO", "LOG"}
	if len(tabs) != len(wantLabels) {
		t.Fatalf("tabs = %d, want %d", len(tabs), len(wantLabels))
	}
	for i, want := range wantLabels {
		if tabs[i].label != want {
			t.Errorf("tab[%d] = %q, want %q", i, tabs[i].label, want)
		}
	}
	if tabs[0].kind != state.TabKindTranscript {
		t.Errorf("transcript kind = %q, want %q", tabs[0].kind, state.TabKindTranscript)
	}
	if tabs[2].kind != tabKindInfo {
		t.Errorf("info kind = %q, want %q", tabs[2].kind, tabKindInfo)
	}
	if tabs[3].kind != tabKindLog {
		t.Errorf("log kind = %q, want %q", tabs[3].kind, tabKindLog)
	}
}

func TestBuildTabList_NoDriverTabsStillShowsInfoAndLog(t *testing.T) {
	current := &proto.SessionInfo{ID: "s1"} // empty View
	tabs := buildTabList(map[string]*tabState{}, current, "/var/log/roost.log")

	if len(tabs) != 2 {
		t.Fatalf("tabs = %d, want 2 (INFO + LOG)", len(tabs))
	}
	if tabs[0].label != "INFO" || tabs[1].label != "LOG" {
		t.Errorf("tab labels = [%q %q], want [INFO LOG]", tabs[0].label, tabs[1].label)
	}
}

func TestBuildTabList_SuppressInfoHidesInfoTab(t *testing.T) {
	current := &proto.SessionInfo{
		ID: "s1",
		View: state.View{
			SuppressInfo: true,
		},
	}
	tabs := buildTabList(map[string]*tabState{}, current, "/var/log/roost.log")

	if len(tabs) != 1 {
		t.Fatalf("tabs = %d, want 1 (LOG only)", len(tabs))
	}
	if tabs[0].label != "LOG" {
		t.Errorf("tab label = %q, want LOG", tabs[0].label)
	}
}

func TestBuildTabList_NoCurrentSessionShowsLogOnly(t *testing.T) {
	tabs := buildTabList(map[string]*tabState{}, nil, "/var/log/roost.log")
	if len(tabs) != 1 || tabs[0].label != "LOG" {
		t.Errorf("tabs = %+v, want [LOG]", tabLabels(tabs))
	}
}

func TestBuildTabList_ReusesTabStateOnSameLabelPathKind(t *testing.T) {
	prev := &tabState{label: "EVENTS", logPath: "/tmp/x.log", kind: state.TabKindText, offset: 100, buf: "partial"}
	current := &proto.SessionInfo{
		ID: "s1",
		View: state.View{
			LogTabs: []state.LogTab{
				{Label: "EVENTS", Path: "/tmp/x.log", Kind: state.TabKindText},
			},
		},
	}
	prevMap := map[string]*tabState{"EVENTS": prev}
	tabs := buildTabList(prevMap, current, "/var/log/roost.log")

	// Find the EVENTS tab and confirm it is the same instance.
	var got *tabState
	for _, t := range tabs {
		if t.label == "EVENTS" {
			got = t
			break
		}
	}
	if got != prev {
		t.Errorf("EVENTS tab not reused: got %p, want %p", got, prev)
	}
	if got.offset != 100 || got.buf != "partial" {
		t.Errorf("reused state lost: offset=%d buf=%q", got.offset, got.buf)
	}
}

func tabLabels(tabs []*tabState) []string {
	out := make([]string, len(tabs))
	for i, t := range tabs {
		out[i] = t.label
	}
	return out
}
