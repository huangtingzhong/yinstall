package os

// DefaultShmmni is the usual kernel.shmmni for database hosts.
const DefaultShmmni int64 = 4096

// ComputeKernelShmSizing returns kernel.shmmax (bytes), kernel.shmall (pages), kernel.shmmni
// following YashanDB installer guidance: shmmax within 50%–90% of physical RAM and above
// estimated SGA when useMaxRAMOnly is false; when useMaxRAMOnly (standalone OS without
// explicit --db-memory-percent), shmmax is set to 90% of physical RAM.
func ComputeKernelShmSizing(memTotalKB, pageSize int64, useMaxRAMOnly bool, dbMemoryPercent int) (shmmax, shmall, shmmni int64) {
	shmmni = DefaultShmmni
	if pageSize <= 0 {
		pageSize = 4096
	}
	phys := memTotalKB * 1024
	if phys <= 0 {
		return 0, 0, shmmni
	}

	if useMaxRAMOnly {
		shmmax = phys * 90 / 100
		shmall = shmmax / pageSize
		return shmmax, shmall, shmmni
	}

	pct := dbMemoryPercent
	if pct < 1 {
		pct = 1
	}
	if pct > 100 {
		pct = 100
	}

	sga := phys * int64(pct) / 100
	minShm := phys * 50 / 100
	maxShm := phys * 90 / 100
	target := phys * 80 / 100

	shmmax = minInt64(maxShm, maxInt64(minShm, maxInt64(target, sga+1)))
	if shmmax > maxShm {
		shmmax = maxShm
	}
	shmall = shmmax / pageSize
	return shmmax, shmall, shmmni
}

// ShmMeetsDBRequirement reports whether current sysctl values satisfy the same sizing rules
// used for installation. For shmmax the database needs a segment limit strictly greater than
// the estimated SGA when sizing from dbMemoryPercent (not max-RAM-only mode).
func ShmMeetsDBRequirement(memTotalKB, pageSize int64, useMaxRAMOnly bool, dbMemoryPercent int, curShmmax, curShmall int64) (ok bool, reason string) {
	reqShmmax, reqShmall, _ := ComputeKernelShmSizing(memTotalKB, pageSize, useMaxRAMOnly, dbMemoryPercent)
	if reqShmmax <= 0 {
		return false, "invalid MemTotal for shared memory check"
	}
	if curShmmax < reqShmmax {
		return false, "kernel.shmmax is below the required minimum for this host and db-memory-percent"
	}
	if curShmall < reqShmall {
		return false, "kernel.shmall is below the required minimum for this host and db-memory-percent"
	}
	if !useMaxRAMOnly {
		phys := memTotalKB * 1024
		pct := dbMemoryPercent
		if pct < 1 {
			pct = 1
		}
		if pct > 100 {
			pct = 100
		}
		sga := phys * int64(pct) / 100
		if curShmmax <= sga {
			return false, "kernel.shmmax must be greater than estimated database shared memory (MemTotal * db-memory-percent / 100)"
		}
	}
	return true, ""
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
