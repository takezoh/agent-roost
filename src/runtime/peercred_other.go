//go:build !linux

package runtime

import "net"

// checkPeerCred is a no-op on non-Linux platforms; the 0o600 socket
// permission is the only access control layer on those systems.
func checkPeerCred(_ net.Conn) error { return nil }
