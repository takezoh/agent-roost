package driver

import "time"

// refreshBranch re-runs git branch detection if the cached value is
// stale. The driver caches both the result and the directory it was
// detected against, so a swap of working_dir invalidates the cache
// immediately while a stable directory only re-runs git on the
// configured cadence (claudeBranchRefreshInterval).
//
// Called from Tick() inside the existing Active() gate — non-active
// sessions never re-detect (they remain on the cached value, which
// will be refreshed within one tick of becoming active again).
//
// Single-threaded: callers serialize access through the driverActor
// goroutine. The git command itself runs synchronously inside that
// goroutine, so a slow detection blocks only this driver's other
// commands (View / Tick / etc.) — not other drivers.
func (d *claudeDriver) refreshBranch(now time.Time, projectFallback string) {
	if d.detectBranch == nil {
		return
	}
	target := d.workingDir
	if target == "" {
		target = projectFallback
	}
	if target == "" {
		return
	}
	if target == d.branchTarget && now.Sub(d.branchAt) < claudeBranchRefreshInterval {
		return
	}
	branch := d.detectBranch(target)
	d.branchTag = branch
	d.branchTarget = target
	d.branchAt = now
}
