package core

import (
	"testing"
	"time"

	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/session/driver"
)

func TestBuildSessionInfos_PullsViewFromDriver(t *testing.T) {
	// Build a real DriverService with a fake driver registered under "fake".
	registry := driver.NewRegistry(newFakeDriverFactory("missing"))
	registry.Register("fake", newFakeDriverFactory("hello"))
	drivers := driver.NewDriverService(registry, driver.Deps{})

	sess := &session.Session{
		ID:        "s1",
		Project:   "/proj",
		Command:   "fake",
		WindowID:  "@1",
		CreatedAt: time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC),
	}
	drivers.Create(sess.ID, sess.Command, fakeSessionContextWithID("s1"))

	infos := BuildSessionInfos([]*session.Session{sess}, drivers)
	if len(infos) != 1 {
		t.Fatalf("infos = %d, want 1", len(infos))
	}
	info := infos[0]
	if info.ID != "s1" || info.Project != "/proj" || info.Command != "fake" || info.WindowID != "@1" {
		t.Errorf("generic fields not copied: %+v", info)
	}
	if info.View.Card.Title != "hello" {
		t.Errorf("View.Card.Title = %q, want hello (driver-provided)", info.View.Card.Title)
	}
	if info.State != driver.StatusIdle {
		t.Errorf("State = %v, want idle", info.State)
	}
}

// fakeDriver is a minimal Driver implementation for protocol tests.
type fakeDriver struct {
	title string
}

func newFakeDriverFactory(title string) driver.Factory {
	return func(deps driver.Deps) driver.Driver {
		return &fakeDriver{title: title}
	}
}

func (d *fakeDriver) Name() string                                     { return "fake" }
func (d *fakeDriver) DisplayName() string                              { return "fake" }
func (d *fakeDriver) MarkSpawned()                                     {}
func (d *fakeDriver) Tick(time.Time, driver.WindowInfo)                {}
func (d *fakeDriver) HandleEvent(driver.AgentEvent) bool               { return false }
func (d *fakeDriver) Close()                                           {}
func (d *fakeDriver) Status() (driver.StatusInfo, bool) {
	return driver.StatusInfo{Status: driver.StatusIdle, ChangedAt: time.Now()}, true
}
func (d *fakeDriver) View() driver.SessionView {
	return driver.SessionView{
		Card: driver.CardView{Title: d.title},
	}
}
func (d *fakeDriver) PersistedState() map[string]string         { return nil }
func (d *fakeDriver) RestorePersistedState(map[string]string)   {}
func (d *fakeDriver) SpawnCommand(base string) string           { return base }

// fakeSessionContextWithID is a minimal SessionContext for tests.
type fakeSessionContextStub struct{ id string }

func (f fakeSessionContextStub) Active() bool { return false }
func (f fakeSessionContextStub) ID() string   { return f.id }

func fakeSessionContextWithID(id string) driver.SessionContext {
	return fakeSessionContextStub{id: id}
}
