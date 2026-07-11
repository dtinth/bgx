//go:build linux

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// getProcessStats reads CPU time and resident memory for a pid from /proc.
// Returns zero values when the information is unavailable.
func getProcessStats(pid int) (cpuSeconds float64, memBytes int64) {
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		cpuSeconds, _ = parseStatCPU(string(data))
	}

	statmData, err := os.ReadFile(fmt.Sprintf("/proc/%d/statm", pid))
	if err != nil {
		return cpuSeconds, 0
	}
	statmFields := strings.Fields(string(statmData))
	if len(statmFields) < 2 {
		return cpuSeconds, 0
	}
	resident, _ := strconv.ParseUint(statmFields[1], 10, 64)
	memBytes = int64(resident) * int64(syscall.Getpagesize())
	return cpuSeconds, memBytes
}

// parseStatCPU extracts cumulative CPU seconds (utime + stime) from the
// contents of /proc/<pid>/stat. The comm field (field 2) is wrapped in
// parentheses and may itself contain spaces or parentheses, so we split on the
// last ')' rather than on whitespace. Counting from the state field that
// follows comm, utime and stime are at indices 11 and 12.
func parseStatCPU(stat string) (float64, bool) {
	rparen := strings.LastIndexByte(stat, ')')
	if rparen < 0 || rparen+2 >= len(stat) {
		return 0, false
	}
	fields := strings.Fields(stat[rparen+2:])
	if len(fields) < 13 {
		return 0, false
	}
	utime, err1 := strconv.ParseUint(fields[11], 10, 64)
	stime, err2 := strconv.ParseUint(fields[12], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	const clockTicks = 100 // typical CLK_TCK on Linux
	return float64(utime+stime) / clockTicks, true
}
