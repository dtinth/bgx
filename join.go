package main

import (
	"database/sql"
	"fmt"
	"os"
	"time"
)

// parseJoinArgs parses `join` arguments of the form: --task-name NAME
func parseJoinArgs(args []string) (taskName string, err error) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--task-name":
			if i+1 >= len(args) {
				return "", fmt.Errorf("--task-name requires an argument")
			}
			taskName = args[i+1]
			i++
		default:
			return "", fmt.Errorf("unexpected argument %q\nUsage: bgx join --task-name NAME", args[i])
		}
	}
	if taskName == "" {
		return "", fmt.Errorf("--task-name is required")
	}
	return taskName, nil
}

func runJoin(args []string) (int, error) {
	taskName, err := parseJoinArgs(args)
	if err != nil {
		return 1, err
	}

	db, err := openDB()
	if err != nil {
		return 1, err
	}
	defer db.Close()

	exists, err := taskExists(db, taskName)
	if err != nil {
		return 1, fmt.Errorf("failed to look up task: %w", err)
	}
	if !exists {
		return 1, fmt.Errorf("task %q not found (BGX_DB=%s)", taskName, getDBPath())
	}

	return processEvents(db, taskName)
}

// processEvents streams a task's output to stdout/stderr and returns its exit
// code. It polls the database, advancing a monotonic id cursor, until it sees
// the exit event or the task stops emitting events for HeartbeatTimeout.
func processEvents(db *sql.DB, taskName string) (int, error) {
	var lastID int64
	lastEventTime := time.Now()
	exitCode := 0
	hasExited := false

	for {
		events, err := readEventsAfter(db, taskName, lastID)
		if err != nil {
			return 1, fmt.Errorf("failed to read events: %w", err)
		}

		for _, e := range events {
			lastID = e.ID
			switch e.Type {
			case EventTypeStdout:
				fmt.Print(e.Data)
			case EventTypeStderr:
				fmt.Fprint(os.Stderr, e.Data)
			case EventTypeExit:
				exitCode = e.Code
				hasExited = true
			}
		}

		if hasExited {
			return exitCode, nil
		}
		if len(events) > 0 {
			lastEventTime = time.Now()
		} else if time.Since(lastEventTime) > HeartbeatTimeout {
			return 1, fmt.Errorf("heartbeat timeout: no events received for %v", HeartbeatTimeout)
		}

		time.Sleep(JoinPollInterval)
	}
}
