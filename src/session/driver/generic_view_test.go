package driver

import "testing"

func TestGenericDriver_ViewProducesCommandTagOnly(t *testing.T) {
	d := newGenericFactory("bash")(Deps{}).(*genericDriver)
	view := d.View()
	if len(view.Card.Tags) != 1 {
		t.Fatalf("tags = %+v, want 1 (command tag only)", view.Card.Tags)
	}
	if view.Card.Tags[0].Text != "bash" {
		t.Errorf("tag text = %q, want bash", view.Card.Tags[0].Text)
	}
	if len(view.LogTabs) != 0 {
		t.Errorf("LogTabs = %+v, want none", view.LogTabs)
	}
	if view.InfoExtras != nil {
		t.Errorf("InfoExtras = %+v, want nil", view.InfoExtras)
	}
	if view.StatusLine != "" {
		t.Errorf("StatusLine = %q, want empty", view.StatusLine)
	}
}

func TestGenericDriver_FallbackEmitsNoCommandTag(t *testing.T) {
	// The DefaultRegistry fallback factory has no name, so it must NOT
	// emit an empty colored chip — emit no tags at all instead.
	d := newGenericFactory("")(Deps{}).(*genericDriver)
	view := d.View()
	if len(view.Card.Tags) != 0 {
		t.Errorf("unnamed fallback driver should produce no tags, got %+v", view.Card.Tags)
	}
}
