//go:build !experimental

package features

import "testing"

func TestExperimentalConst(t *testing.T) {
	if Experimental {
		t.Error("Experimental = true in default build, want false")
	}
}
