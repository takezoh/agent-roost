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
			// Firecracker requires the VM to be paused before snapshotting.
			if cfg.Runs > 1 {
				fmt.Println("  pausing VM for snapshot…")
				if err := vm.Pause(); err != nil {
					fmt.Printf("  pause failed (snap-start skipped): %v\n", err)
				} else {
					fmt.Println("  creating snapshot…")
					if err := vm.CreateSnapshot(); err != nil {
						fmt.Printf("  snapshot failed (snap-start skipped): %v\n", err)
						cfg.snapPath = ""
						cfg.memPath = ""
					} else {
						// Move snapshot files to a fixed location before Stop()
						// deletes the VM's temp dir (which contains them).
						// os.Rename is a fast rename on the same filesystem.
						globalSnap := filepath.Join(os.TempDir(), "roost-poc-snap.vmstate")
						globalMem := filepath.Join(os.TempDir(), "roost-poc-snap.mem")
						if err := os.Rename(vm.snapPath, globalSnap); err != nil {
							fmt.Printf("  snapshot mv failed: %v\n", err)
							cfg.snapPath = ""
							cfg.memPath = ""
						} else if err := os.Rename(vm.memPath, globalMem); err != nil {
							fmt.Printf("  mem mv failed: %v\n", err)
							cfg.snapPath = ""
							cfg.memPath = ""
						} else {
							cfg.snapPath = globalSnap
							cfg.memPath = globalMem
							cfg.vsockSnapPath = vm.vsockPath
						}
					}
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

// startVMFromSnap starts a bare Firecracker process (no boot resources
// configured) ready for snapshot/load.  Any prior configuration would
// cause Firecracker to reject the load with an error.
func startVMFromSnap(id string, cfg *Config) (*VM, error) {
	return startVMForSnap(id, cfg, cfg.snapPath, cfg.memPath)
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
