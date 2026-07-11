package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetDBPath(t *testing.T) {
	// BGX_DB always wins, verbatim.
	t.Run("BGX_DB takes precedence", func(t *testing.T) {
		t.Setenv("RUNNER_TEMP", "/runner/tmp")
		t.Setenv("BGX_DB", "/explicit/path.db")
		if got := getDBPath(); got != "/explicit/path.db" {
			t.Errorf("got %q, want /explicit/path.db", got)
		}
	})

	// With BGX_DB unset, RUNNER_TEMP (set by CI runners) is used for per-job
	// isolation.
	t.Run("RUNNER_TEMP when BGX_DB unset", func(t *testing.T) {
		t.Setenv("BGX_DB", "")
		t.Setenv("RUNNER_TEMP", "/runner/tmp")
		want := filepath.Join("/runner/tmp", "bgx.db")
		if got := getDBPath(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	// With neither set, fall back to the system temp directory.
	t.Run("system tempdir fallback", func(t *testing.T) {
		t.Setenv("BGX_DB", "")
		t.Setenv("RUNNER_TEMP", "")
		want := filepath.Join(os.TempDir(), "bgx.db")
		if got := getDBPath(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}
