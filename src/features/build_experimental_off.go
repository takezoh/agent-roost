//go:build !experimental

package features

// Experimental is false in the default (release) build. Any
// `if features.Experimental { ... }` branch is eliminated by the Go
// compiler's dead code elimination — the experimental code does not
// exist in the binary at all.
const Experimental = false
