//go:build !linux

package main

// getProcessStats reports CPU time and resident memory for a pid. Resource
// monitoring reads /proc, which only exists on Linux, so on other platforms
// (macOS, Windows) it reports zero and heartbeats simply carry no stats.
func getProcessStats(pid int) (cpuSeconds float64, memBytes int64) {
	return 0, 0
}
