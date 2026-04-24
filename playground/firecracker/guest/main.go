// guest-signal is a tiny statically-linked binary that runs inside the
// Firecracker VM.  It waits for the system to settle, then connects to
// the host via AF_VSOCK (CID 2, port 52000) and sends "ready\n".
// The host measures the time from InstanceStart to this event.
//
// Build (from host):
//   GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o guest-signal .
package main

import (
	"syscall"
	"time"
	"unsafe"
)

const (
	afVsock   = 40    // AF_VSOCK (linux)
	hostCID   = 2     // host CID in Firecracker vsock model
	readyPort = 52000 // must match host listener suffix
)

// rawSockaddrVM mirrors struct sockaddr_vm from <linux/vm_sockets.h>.
type rawSockaddrVM struct {
	Family    uint16
	Reserved1 uint16
	Port      uint32
	CID       uint32
	_         [4]byte
}

func main() {
	// Give the kernel and initrd time to settle before attempting the
	// vsock connection.  500 ms is conservative; reduce if p50 allows.
	time.Sleep(500 * time.Millisecond)

	fd, err := syscall.Socket(afVsock, syscall.SOCK_STREAM, 0)
	if err != nil {
		return
	}
	defer syscall.Close(fd) //nolint:errcheck

	sa := rawSockaddrVM{Family: afVsock, Port: readyPort, CID: hostCID}
	_, _, errno := syscall.Syscall(
		syscall.SYS_CONNECT,
		uintptr(fd),
		uintptr(unsafe.Pointer(&sa)),
		unsafe.Sizeof(sa),
	)
	if errno != 0 {
		return
	}
	syscall.Write(fd, []byte("ready\n")) //nolint:errcheck
}
