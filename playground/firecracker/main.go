// Package main is a Firecracker PoC measurement spike for agent-roost.
//
// It measures cold-start, snapshot-start, fleet (parallel) start, memory
// RSS, teardown latency, and image footprint for a minimal Alpine-based VM.
//
// Prerequisites (run setup.sh first):
//
//	~/.roost/images/firecracker     – Firecracker v1.11.0 binary
//	~/.roost/images/vmlinux         – Linux kernel image
//	~/.roost/images/rootfs.ext4     – Alpine rootfs with guest-signal binary
//
// Usage:
//
//	go run . [--runs N] [--fleet N] [--timeout 30s]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"
)

// Config holds all tunable parameters for the measurement run.
type Config struct {
	FCBin        string
	KernelPath   string
	RootfsPath   string
	Runs         int
	FleetN       int
	ReadyTimeout time.Duration

	// set internally after snapshot creation
	snapPath     string
	memPath      string
	vsockSnapPath string // vsock UDS base path of the source VM (for snap restore cleanup)
}

func defaultImageDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".roost", "images")
}

func main() {
	imgDir := defaultImageDir()

	var cfg Config
	flag.StringVar(&cfg.FCBin, "firecracker", filepath.Join(imgDir, "firecracker"), "path to firecracker binary")
	flag.StringVar(&cfg.KernelPath, "kernel", filepath.Join(imgDir, "vmlinux"), "path to vmlinux kernel")
	flag.StringVar(&cfg.RootfsPath, "rootfs", filepath.Join(imgDir, "rootfs.ext4"), "path to rootfs ext4 image")
	flag.IntVar(&cfg.Runs, "runs", 5, "number of cold-start measurement runs")
	flag.IntVar(&cfg.FleetN, "fleet", 5, "number of VMs to start in parallel for fleet test")
	flag.DurationVar(&cfg.ReadyTimeout, "timeout", 30*time.Second, "per-VM ready signal timeout")
	flag.Parse()

	if err := checkPrereqs(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "prerequisites missing:", err)
		fmt.Fprintln(os.Stderr, "run: bash setup.sh")
		os.Exit(1)
	}

	fmt.Println("=== Firecracker PoC — agent-roost sandbox measurement ===")
	fmt.Printf("kernel  : %s\nrootfs  : %s\nruns    : %d\nfleet   : %d\n\n",
		cfg.KernelPath, cfg.RootfsPath, cfg.Runs, cfg.FleetN)

	res, err := run(&cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "measurement failed:", err)
		os.Exit(1)
	}

	printReport(res)
	writeReportMD(res)
}

func checkPrereqs(cfg Config) error {
	for _, path := range []string{cfg.FCBin, cfg.KernelPath, cfg.RootfsPath} {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	return nil
}

func printReport(res Result) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "\n=== Results ===")
	fmt.Fprintf(w, "Cold start p50\t%s\n", pct(res.ColdStart, 0.5).Round(time.Millisecond))
	fmt.Fprintf(w, "Cold start p99\t%s\n", pct(res.ColdStart, 0.99).Round(time.Millisecond))
	if len(res.SnapStart) > 0 {
		fmt.Fprintf(w, "Snap start p50\t%s\n", pct(res.SnapStart, 0.5).Round(time.Millisecond))
		fmt.Fprintf(w, "Snap start p99\t%s\n", pct(res.SnapStart, 0.99).Round(time.Millisecond))
	} else {
		fmt.Fprintf(w, "Snap start\t(skipped)\n")
	}
	fmt.Fprintf(w, "Fleet %d VMs\t%s\n", res.FleetN, res.FleetStart.Round(time.Millisecond))
	fmt.Fprintf(w, "Memory RSS\t%.1f MiB/VM\n", mean(res.MemoryMiB))
	fmt.Fprintf(w, "Teardown p50\t%s\n", pct(res.Teardown, 0.5).Round(time.Millisecond))
	fmt.Fprintf(w, "Image footprint\t%.1f MiB (kernel+rootfs)\n", res.FootprintMiB)
	w.Flush()

	fmt.Println("\n=== GO / NO-GO thresholds ===")
	cold50 := pct(res.ColdStart, 0.5)
	cold99 := pct(res.ColdStart, 0.99)
	snap50 := pct(res.SnapStart, 0.5)
	snap99 := pct(res.SnapStart, 0.99)
	checkThreshold("Cold p50 < 500ms", cold50 < 500*time.Millisecond, cold50)
	checkThreshold("Cold p99 < 1s", cold99 < time.Second, cold99)
	if len(res.SnapStart) > 0 {
		checkThreshold("Snap p50 < 200ms", snap50 < 200*time.Millisecond, snap50)
		checkThreshold("Snap p99 < 500ms", snap99 < 500*time.Millisecond, snap99)
	}
	checkThreshold("Memory < 64 MiB/VM", mean(res.MemoryMiB) < 64, mean(res.MemoryMiB))
}

func checkThreshold(label string, pass bool, val any) {
	mark := "✓"
	if !pass {
		mark = "✗"
	}
	fmt.Printf("  %s %s  (%v)\n", mark, label, val)
}

func writeReportMD(res Result) {
	path := "REPORT.md"
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "could not write REPORT.md:", err)
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "# Firecracker PoC Report\n\n")
	fmt.Fprintf(f, "| Metric | Value | Target | Pass? |\n")
	fmt.Fprintf(f, "|--------|-------|--------|-------|\n")
	cold50 := pct(res.ColdStart, 0.5)
	cold99 := pct(res.ColdStart, 0.99)
	fmt.Fprintf(f, "| Cold start p50 | %s | <500ms | %s |\n", cold50.Round(time.Millisecond), yn(cold50 < 500*time.Millisecond))
	fmt.Fprintf(f, "| Cold start p99 | %s | <1s | %s |\n", cold99.Round(time.Millisecond), yn(cold99 < time.Second))
	if len(res.SnapStart) > 0 {
		snap50 := pct(res.SnapStart, 0.5)
		snap99 := pct(res.SnapStart, 0.99)
		fmt.Fprintf(f, "| Snap start p50 | %s | <200ms | %s |\n", snap50.Round(time.Millisecond), yn(snap50 < 200*time.Millisecond))
		fmt.Fprintf(f, "| Snap start p99 | %s | <500ms | %s |\n", snap99.Round(time.Millisecond), yn(snap99 < 500*time.Millisecond))
	}
	fmt.Fprintf(f, "| Fleet %d VMs | %s | — | — |\n", res.FleetN, res.FleetStart.Round(time.Millisecond))
	fmt.Fprintf(f, "| Memory RSS | %.1f MiB | <64 MiB | %s |\n", mean(res.MemoryMiB), yn(mean(res.MemoryMiB) < 64))
	fmt.Fprintf(f, "| Teardown p50 | %s | <200ms | %s |\n", pct(res.Teardown, 0.5).Round(time.Millisecond), yn(pct(res.Teardown, 0.5) < 200*time.Millisecond))
	fmt.Fprintf(f, "| Image footprint | %.1f MiB | <200 MiB | %s |\n", res.FootprintMiB, yn(res.FootprintMiB < 200))
	fmt.Fprintf(f, "\n## Integration points\n\n")
	fmt.Fprintln(f, "- [ ] vsock IPC: `roost event` in guest → host daemon socket")
	fmt.Fprintln(f, "- [ ] virtio-blk worktree FS share: host write, guest read")
	fmt.Fprintln(f, "- [ ] transcript fsnotify: guest write, host fsnotify fires")
	fmt.Fprintln(f, "- [ ] tmux attach: serial pty bridge to pane")
	fmt.Fprintf(f, "\n## Verdict\n\n<!-- fill after running -->\n")
	fmt.Println("\nwrote REPORT.md")
}

func yn(b bool) string {
	if b {
		return "✓"
	}
	return "✗"
}
