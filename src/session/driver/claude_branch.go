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
func (d *claudeDriver) refreshBranch(now time.Time, projectFallback string) {
	if d.detectBranch == nil {
		return
	}
	d.mu.Lock()
	target := d.workingDir
	if target == "" {
		target = projectFallback
	}
	if target == "" {
		d.mu.Unlock()
		return
	}
	if target == d.branchTarget && now.Sub(d.branchAt) < claudeBranchRefreshInterval {
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()

	branch := d.detectBranch(target)

	d.mu.Lock()
	d.branchTag = branch
	d.branchTarget = target
	d.branchAt = now
	d.mu.Unlock()
}
