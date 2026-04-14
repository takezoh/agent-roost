//go:build experimental

package features

// Experimental is true when the binary was built with -tags experimental.
// Because this is a const, any `if features.Experimental { ... }` branch in
// the off-side build is eliminated by the Go compiler's dead code elimination,
// making this mechanism equivalent to a C #if/#endif guard.
const Experimental = true
