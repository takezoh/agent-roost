//go:build linux

package runtime

import (
	"fmt"
	"net"
	"os"
	"syscall"
)

// checkPeerCred verifies that the peer on a Unix domain socket belongs to
// the same uid as the current process. Rejects connections from other users
// even if they somehow bypass the 0o600 socket permission (e.g. root).
func checkPeerCred(conn net.Conn) error {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return nil
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return fmt.Errorf("peercred: SyscallConn: %w", err)
	}
	var ucred *syscall.Ucred
	var ctrlErr error
	if err := raw.Control(func(fd uintptr) {
		ucred, ctrlErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return fmt.Errorf("peercred: Control: %w", err)
	}
	if ctrlErr != nil {
		return fmt.Errorf("peercred: SO_PEERCRED: %w", ctrlErr)
	}
	if int(ucred.Uid) != os.Getuid() {
		return fmt.Errorf("peercred: uid mismatch: peer=%d self=%d", ucred.Uid, os.Getuid())
	}
	return nil
}
