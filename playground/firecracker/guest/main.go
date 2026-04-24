// guest-signal runs as PID 1 (init) inside the Firecracker VM.
// It mounts the essential pseudo-filesystems, signals the host that
// the VM is ready via AF_VSOCK, then sleeps indefinitely so the
// kernel does not panic (init must never exit).
//
// Build (from host):
//   GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o guest-signal .
package main

import (
	"os"
	"syscall"
	"time"
	"unsafe"
)

const (
	afVsock   = 40    // AF_VSOCK
	hostCID   = 2     // host CID in Firecracker's vsock proxy model
	readyPort = 52000 // must match the host-side listener suffix
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
	// Create mount points if the minimal rootfs omitted them.
	for _, d := range []string{"/dev", "/proc", "/sys", "/tmp"} {
		_ = os.MkdirAll(d, 0o755)
	}

	// Mount pseudo-filesystems.  Errors are ignored; the kernel may
	// have already mounted some, or the driver may not be present.
	syscall.Mount("devtmpfs", "/dev", "devtmpfs", 0, "") //nolint:errcheck
	syscall.Mount("proc", "/proc", "proc", 0, "")        //nolint:errcheck
	syscall.Mount("sysfs", "/sys", "sysfs", 0, "")       //nolint:errcheck

	// Small pause to let the virtio-vsock driver finish init inside
	// the kernel before we try to connect.
	time.Sleep(100 * time.Millisecond)

	signalReady()

	// PID 1 must never exit — a kernel panic would follow.
	select {}
}

func signalReady() {
	fd, err := syscall.Socket(afVsock, syscall.SOCK_STREAM, 0)
	if err != nil {
		return
	}
	defer syscall.Close(fd) //nolint:errcheck

	sa := rawSockaddrVM{Family: afVsock, Port: readyPort, CID: hostCID}
	syscall.Syscall( //nolint:errcheck
		syscall.SYS_CONNECT,
		uintptr(fd),
		uintptr(unsafe.Pointer(&sa)),
		unsafe.Sizeof(sa),
	)
	syscall.Write(fd, []byte("ready\n")) //nolint:errcheck
}
