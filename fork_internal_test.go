package main

import "testing"

func TestParseStatCPU(t *testing.T) {
	// Fields after comm: state ppid pgrp session tty_nr tpgid flags minflt
	// cminflt majflt cmajflt utime stime ... => utime/stime at indices 11/12.
	tests := []struct {
		name string
		stat string
		want float64
		ok   bool
	}{
		{
			name: "simple comm",
			stat: "4242 (bash) S 1 2 3 4 5 6 7 8 9 10 200 100 0 0",
			want: 3.0, // (200 + 100) / 100
			ok:   true,
		},
		{
			name: "comm with spaces",
			stat: "4242 (my program) R 1 2 3 4 5 6 7 8 9 10 50 50 0 0",
			want: 1.0, // (50 + 50) / 100
			ok:   true,
		},
		{
			name: "comm with parentheses",
			stat: "4242 (weird)name) S 1 2 3 4 5 6 7 8 9 10 0 0",
			want: 0.0,
			ok:   true,
		},
		{
			name: "no closing paren",
			stat: "4242 bash S 1 2 3",
			want: 0,
			ok:   false,
		},
		{
			name: "too few fields",
			stat: "4242 (bash) S 1 2 3",
			want: 0,
			ok:   false,
		},
		{
			name: "garbage",
			stat: "not a stat line",
			want: 0,
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseStatCPU(tt.stat)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("cpuSeconds = %v, want %v", got, tt.want)
			}
		})
	}
}
