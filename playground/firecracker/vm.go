package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	vsockReadyPort = 52000
	guestCID       = 3  // arbitrary non-2 CID for the guest
	vcpus          = 1
	memMiB         = 128
)

// VM represents one running Firecracker micro-VM.
type VM struct {
	id          string
	apiSockPath string
	vsockPath   string // base path; host listener uses vsockPath+"_"+port
	snapPath    string
	memPath     string
	proc        *exec.Cmd
	fc          *fcClient
	cfg         *Config
}

// startVMForSnap launches a fresh Firecracker process and waits for the
// API to be ready, but does NOT configure any boot resources.  This is
// the correct way to prepare for snapshot/load: configuring boot resources
// before loading a snapshot is an error in Firecracker.
func startVMForSnap(id string, cfg *Config, snapPath, memPath string) (*VM, error) {
	dir := filepath.Join(os.TempDir(), "roost-poc-"+id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	apiSock := filepath.Join(dir, "api.sock")
	vsockPath := filepath.Join(dir, "vsock.sock")
	_ = os.Remove(apiSock)
	_ = os.Remove(vsockPath)

	// Remove stale vsock socket files from the source VM so Firecracker can
	// bind to the same path when restoring the snapshot.
	if cfg.vsockSnapPath != "" {
		_ = os.Remove(cfg.vsockSnapPath)
		_ = os.Remove(cfg.vsockSnapPath + "_" + strconv.Itoa(vsockReadyPort))
	}

	logPath := filepath.Join(dir, "fc.log")
	lf, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("create fc log: %w", err)
	}
	lf.Close()

	cmd := exec.Command(cfg.FCBin,
		"--api-sock", apiSock,
		"--log-path", logPath,
		"--level", "Warning",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start firecracker: %w", err)
	}

	v := &VM{
		id: id, apiSockPath: apiSock,
		vsockPath: vsockPath, snapPath: snapPath, memPath: memPath,
		proc: cmd, cfg: cfg,
	}
	v.fc = newFCClient(apiSock)

	if err := v.fc.waitAPIReady(3 * time.Second); err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	return v, nil
}

// startVM launches the firecracker process and configures it via API.
// It does NOT send InstanceStart; call Boot() for that.
func startVM(id string, cfg *Config) (*VM, error) {
	dir := filepath.Join(os.TempDir(), "roost-poc-"+id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	apiSock := filepath.Join(dir, "api.sock")
	// vsock path must be outside the VM's temp dir so it survives Stop()'s
	// RemoveAll — the snapshot records this path and restore needs to bind to it.
	vsockPath := filepath.Join(os.TempDir(), "roost-poc-vsock-"+id+".sock")
	snapPath := filepath.Join(dir, "snap.vmstate")
	memPath := filepath.Join(dir, "snap.mem")

	// Remove stale sockets from a previous run.
	_ = os.Remove(apiSock)
	_ = os.Remove(vsockPath)

	// Pre-create the log file; Firecracker requires the file to already
	// exist when --log-path points to a regular file (not a FIFO).
	logPath := filepath.Join(dir, "fc.log")
	lf, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("create fc log: %w", err)
	}
	lf.Close()

	cmd := exec.Command(cfg.FCBin,
		"--api-sock", apiSock,
		"--log-path", logPath,
		"--level", "Warning",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start firecracker: %w", err)
	}

	v := &VM{
		id: id, apiSockPath: apiSock,
		vsockPath: vsockPath, snapPath: snapPath, memPath: memPath,
		proc: cmd, cfg: cfg,
	}
	v.fc = newFCClient(apiSock)

	if err := v.fc.waitAPIReady(3 * time.Second); err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	if err := v.configure(); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("configure VM %s: %w", id, err)
	}
	return v, nil
}

// configure pushes kernel/rootfs/vsock/machine config via the REST API.
func (v *VM) configure() error {
	if err := v.fc.put("/boot-source", bootSource{
		KernelImagePath: v.cfg.KernelPath,
		BootArgs:        "console=ttyS0 reboot=k panic=1 pci=off",
	}); err != nil {
		return err
	}
	if err := v.fc.put("/drives/rootfs", drive{
		DriveID:      "rootfs",
		PathOnHost:   v.cfg.RootfsPath,
		IsRootDevice: true,
		IsReadOnly:   false,
	}); err != nil {
		return err
	}
	if err := v.fc.put("/vsock", vsockDevice{
		GuestCID: guestCID,
		UDSPath:  v.vsockPath,
	}); err != nil {
		return err
	}
	return v.fc.put("/machine-config", machineConfig{
		VCPUCount:  vcpus,
		MemSizeMiB: memMiB,
	})
}

// Boot sends InstanceStart and returns elapsed time to guest-ready signal.
// It starts the host-side vsock listener before sending InstanceStart so
// the connection is never dropped.
func (v *VM) Boot() (time.Duration, error) {
	listenerPath := v.vsockPath + "_" + strconv.Itoa(vsockReadyPort)
	_ = os.Remove(listenerPath)
	ln, err := net.Listen("unix", listenerPath)
	if err != nil {
		return 0, fmt.Errorf("vsock listener: %w", err)
	}
	defer ln.Close()

	t0 := time.Now()
	if err := v.fc.put("/actions", action{ActionType: "InstanceStart"}); err != nil {
		return 0, err
	}

	// Wait for the guest-signal binary to connect and write "ready\n".
	_ = ln.(*net.UnixListener).SetDeadline(time.Now().Add(v.cfg.ReadyTimeout))
	conn, err := ln.Accept()
	if err != nil {
		return 0, fmt.Errorf("waiting for ready signal: %w", err)
	}
	elapsed := time.Since(t0)
	conn.Close()
	return elapsed, nil
}

// vmState is the body for PATCH /vm (pause/resume).
type vmState struct {
	State string `json:"state"`
}

// Pause suspends the running VM.  Required before CreateSnapshot.
// Firecracker v1.x uses PATCH /vm, not /actions.
func (v *VM) Pause() error {
	return v.fc.patch("/vm", vmState{State: "Paused"})
}

// CreateSnapshot saves a full VM snapshot for use in SnapBoot.
// The VM must be paused before calling this.
func (v *VM) CreateSnapshot() error {
	return v.fc.put("/snapshot/create", snapshotCreate{
		SnapshotType: "Full",
		SnapshotPath: v.snapPath,
		MemFilePath:  v.memPath,
	})
}

// SnapBoot restores from a previously saved snapshot and measures the
// time for the snapshot/load API call to complete (which blocks until
// the guest is resumed and running).  No vsock signal is needed because
// the guest resumes exactly where it was paused; it never re-executes
// the signalling code.
func (v *VM) SnapBoot() (time.Duration, error) {
	t0 := time.Now()
	if err := v.fc.put("/snapshot/load", snapshotLoad{
		SnapshotPath: v.snapPath,
		MemBackend:   memBackend{BackendPath: v.memPath, BackendType: "File"},
		ResumeVM:     true,
	}); err != nil {
		return 0, err
	}
	return time.Since(t0), nil
}

// MemoryRSSMiB reads the resident set size of the firecracker process from
// /proc/<pid>/status.
func (v *VM) MemoryRSSMiB() (float64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", v.proc.Process.Pid))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			kb, err := strconv.ParseFloat(fields[1], 64)
			if err != nil {
				return 0, err
			}
			return kb / 1024, nil
		}
	}
	return 0, fmt.Errorf("VmRSS not found in /proc status")
}

// Stop terminates the Firecracker process and removes temp files.
func (v *VM) Stop() time.Duration {
	t0 := time.Now()
	if v.proc != nil && v.proc.Process != nil {
		_ = v.proc.Process.Kill()
		_ = v.proc.Wait()
	}
	elapsed := time.Since(t0)
	_ = os.RemoveAll(filepath.Dir(v.apiSockPath))
	return elapsed
}
