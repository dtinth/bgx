package main

import (
	"errors"
	"fmt"
)

// runExec runs a command in the foreground, mirroring its stdout/stderr to the
// terminal while also recording the full lifecycle (start, output, heartbeats,
// exit) to the shared database. It returns the command's exit code.
//
// Unlike `fork`, nothing is detached: exec blocks until the command finishes
// and exits with the same code. The value it adds over running the command
// directly is observability — the recorded events can be inspected later, or
// the whole database uploaded as a CI artifact for analysis.
func runExec(args []string) (int, error) {
	// exec takes the same arguments as fork: --task-name NAME -- COMMAND...
	taskName, command, err := parseForkArgs(args)
	if err != nil {
		return 1, err
	}

	db, err := openDB()
	if err != nil {
		return 1, err
	}
	defer db.Close()

	// Claim the task name up front, exactly like fork, so a name collision is
	// reported instead of silently appending to another task's log.
	if err := registerTask(db, taskName); err != nil {
		if errors.Is(err, ErrTaskExists) {
			return 1, fmt.Errorf("task %q already exists (BGX_DB=%s)\nUse a different --task-name or remove the database.", taskName, getDBPath())
		}
		return 1, err
	}

	return executeProcess(db, taskName, command, true)
}
