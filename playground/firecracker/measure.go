package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Result holds all measurements from one PoC run.
type Result struct {
	// Cold-start: full kernel boot each time (no snapshot)
	ColdStart []time.Duration

	// Snap-start: restore from snapshot (skips kernel boot)
	SnapStart []time.Duration

	// Fleet: wall-clock for N VMs booted in parallel (cold)
	FleetStart time.Duration
	FleetN     int

	// Memory RSS of the firecracker process while VM is idle
	MemoryMiB []float64

	// Teardown: SIGKILL → process exit
	Teardown []time.Duration

	// Static image footprint on disk
	KernelBytes  int64
	RootfsBytes  int64
	FootprintMiB float64
}

// run executes the full measurement suite.
func run(cfg *Config) (Result, error) {
	var res Result

	// --- footprint ---------------------------------------------------------
	ki, err := os.Stat(cfg.KernelPath)
	if err != nil {
		return res, fmt.Errorf("stat kernel: %w", err)
	}
	ri, err := os.Stat(cfg.RootfsPath)
	if err != nil {
		return res, fmt.Errorf("stat rootfs: %w", err)
	}
	res.KernelBytes = ki.Size()
	res.RootfsBytes = ri.Size()
	res.FootprintMiB = float64(ki.Size()+ri.Size()) / (1024 * 1024)

	// --- cold starts -------------------------------------------------------
	fmt.Printf("cold starts (%d runs)…\n", cfg.Runs)
	for i := range cfg.Runs {
		id := fmt.Sprintf("cold-%d", i)
		vm, err := startVM(id, cfg)
		if err != nil {
			return res, fmt.Errorf("cold start %d: %w", i, err)
		}
		elapsed, err := vm.Boot()
		if err != nil {
			vm.Stop()
			return res, fmt.Errorf("cold boot %d: %w", i, err)
		}
		res.ColdStart = append(res.ColdStart, elapsed)
		fmt.Printf("  run %d: %s\n", i, elapsed.Round(time.Millisecond))

		// Memory measurement while VM is idle after first cold boot.
		if i == 0 {
			rss, err := vm.MemoryRSSMiB()
			if err == nil {
				res.MemoryMiB = append(res.MemoryMiB, rss)
			}

			// Save snapshot from this first boot for snap-start tests.
			if cfg.Runs > 1 {
				fmt.Println("  creating snapshot…")
				if err := vm.CreateSnapshot(); err != nil {
					fmt.Printf("  snapshot failed (snap-start skipped): %v\n", err)
					cfg.snapPath = ""
					cfg.memPath = ""
				} else {
					cfg.snapPath = filepath.Join(os.TempDir(), "roost-poc-"+id, "snap.vmstate")
					cfg.memPath = filepath.Join(os.TempDir(), "roost-poc-"+id, "snap.mem")
				}
			}
		}

		td := vm.Stop()
		res.Teardown = append(res.Teardown, td)
	}

	// --- snap starts -------------------------------------------------------
	if cfg.snapPath != "" {
		fmt.Printf("snap starts (%d runs)…\n", cfg.Runs-1)
		for i := 1; i < cfg.Runs; i++ {
			id := fmt.Sprintf("snap-%d", i)
			vm, err := startVMFromSnap(id, cfg)
			if err != nil {
				fmt.Printf("  snap start %d failed: %v\n", i, err)
				continue
			}
			elapsed, err := vm.SnapBoot()
			if err != nil {
				vm.Stop()
				fmt.Printf("  snap boot %d failed: %v\n", i, err)
				continue
			}
			res.SnapStart = append(res.SnapStart, elapsed)
			fmt.Printf("  run %d: %s\n", i, elapsed.Round(time.Millisecond))
			vm.Stop()
		}
	}

	// --- fleet start -------------------------------------------------------
	fmt.Printf("fleet start (%d parallel VMs)…\n", cfg.FleetN)
	res.FleetN = cfg.FleetN
	var wg sync.WaitGroup
	t0 := time.Now()
	for i := range cfg.FleetN {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := fmt.Sprintf("fleet-%d", idx)
			vm, err := startVM(id, cfg)
			if err != nil {
				fmt.Printf("  fleet VM %d start failed: %v\n", idx, err)
				return
			}
			if _, err := vm.Boot(); err != nil {
				fmt.Printf("  fleet VM %d boot failed: %v\n", idx, err)
			}
			vm.Stop()
		}(i)
	}
	wg.Wait()
	res.FleetStart = time.Since(t0)

	return res, nil
}

// startVMFromSnap starts a fresh firecracker process configured for snapshot
// load (no kernel/rootfs needed in boot-source; restored from snap).
func startVMFromSnap(id string, cfg *Config) (*VM, error) {
	// Snapshot-restore still needs a fresh firecracker process.
	// We reuse startVM but skip configure (snapshot path carries all state).
	vm, err := startVM(id, cfg)
	if err != nil {
		return nil, err
	}
	// Override snap/mem paths to the saved snapshot.
	vm.snapPath = cfg.snapPath
	vm.memPath = cfg.memPath
	return vm, nil
}

// --- statistics helpers --------------------------------------------------

func pct(ds []time.Duration, p float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(ds))
	copy(sorted, ds)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

func mean(fs []float64) float64 {
	if len(fs) == 0 {
		return 0
	}
	var s float64
	for _, v := range fs {
		s += v
	}
	return s / float64(len(fs))
}
