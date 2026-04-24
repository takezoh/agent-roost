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

// startVM launches the firecracker process and configures it via API.
// It does NOT send InstanceStart; call Boot() for that.
func startVM(id string, cfg *Config) (*VM, error) {
	dir := filepath.Join(os.TempDir(), "roost-poc-"+id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	apiSock := filepath.Join(dir, "api.sock")
	vsockPath := filepath.Join(dir, "vsock.sock")
	snapPath := filepath.Join(dir, "snap.vmstate")
	memPath := filepath.Join(dir, "snap.mem")

	// Remove stale sockets from a previous run.
	_ = os.Remove(apiSock)
	_ = os.Remove(vsockPath)

	cmd := exec.Command(cfg.FCBin,
		"--api-sock", apiSock,
		"--log-path", filepath.Join(dir, "fc.log"),
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

// CreateSnapshot saves a full VM snapshot for use in SnapBoot.
func (v *VM) CreateSnapshot() error {
	return v.fc.put("/snapshot/create", snapshotCreate{
		SnapshotType: "Full",
		SnapshotPath: v.snapPath,
		MemFilePath:  v.memPath,
	})
}

// SnapBoot restores + resumes from a previously saved snapshot and returns
// time to guest-ready signal. The VM must be freshly started (not yet booted).
func (v *VM) SnapBoot() (time.Duration, error) {
	listenerPath := v.vsockPath + "_" + strconv.Itoa(vsockReadyPort)
	_ = os.Remove(listenerPath)
	ln, err := net.Listen("unix", listenerPath)
	if err != nil {
		return 0, fmt.Errorf("vsock listener: %w", err)
	}
	defer ln.Close()

	t0 := time.Now()
	if err := v.fc.put("/snapshot/load", snapshotLoad{
		SnapshotPath: v.snapPath,
		MemBackend:   memBackend{BackendPath: v.memPath, BackendType: "File"},
		ResumeVM:     true,
	}); err != nil {
		return 0, err
	}

	_ = ln.(*net.UnixListener).SetDeadline(time.Now().Add(v.cfg.ReadyTimeout))
	conn, err := ln.Accept()
	if err != nil {
		return 0, fmt.Errorf("waiting for ready signal (snap): %w", err)
	}
	elapsed := time.Since(t0)
	conn.Close()
	return elapsed, nil
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
