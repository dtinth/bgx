package main

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// joinConfig holds the output options for a join.
type joinConfig struct {
	group      bool // wrap each task's output in a GitHub Actions ::group:: block
	timestamps bool // prefix each line with the event's recorded time
}

// parseJoinArgs parses `join` arguments of the form:
//
//	--task-name NAME [--task-name NAME ...] [--group] [--timestamps]
//
// Repeating --task-name joins several tasks at once.
func parseJoinArgs(args []string) ([]string, joinConfig, error) {
	var taskNames []string
	var cfg joinConfig
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--task-name":
			if i+1 >= len(args) {
				return nil, cfg, fmt.Errorf("--task-name requires an argument")
			}
			taskNames = append(taskNames, args[i+1])
			i++
		case "--group":
			cfg.group = true
		case "--timestamps":
			cfg.timestamps = true
		default:
			return nil, cfg, fmt.Errorf("unexpected argument %q\nUsage: bgx join --task-name NAME [--task-name NAME ...] [--group] [--timestamps]", args[i])
		}
	}
	if len(taskNames) == 0 {
		return nil, cfg, fmt.Errorf("--task-name is required")
	}
	return taskNames, cfg, nil
}

func runJoin(args []string) (int, error) {
	taskNames, cfg, err := parseJoinArgs(args)
	if err != nil {
		return 1, err
	}

	db, err := openDB()
	if err != nil {
		return 1, err
	}
	defer db.Close()

	for _, name := range taskNames {
		exists, err := taskExists(db, name)
		if err != nil {
			return 1, fmt.Errorf("failed to look up task: %w", err)
		}
		if !exists {
			return 1, fmt.Errorf("task %q not found (BGX_DB=%s)", name, getDBPath())
		}
	}

	// --group must keep each task's lines contiguous, so it drains tasks
	// sequentially. Otherwise multiple tasks stream concurrently, each line
	// tagged with its task name; a single task streams unprefixed.
	if cfg.group {
		return joinGrouped(db, taskNames, cfg)
	}
	var printMu sync.Mutex
	if len(taskNames) == 1 {
		return streamTask(db, taskNames[0], "", cfg, &printMu)
	}
	return joinConcurrent(db, taskNames, cfg, &printMu)
}

// joinConcurrent streams every task at once, each line prefixed with [task],
// returning the first failing task's exit code (non-zero if any failed).
func joinConcurrent(db *sql.DB, taskNames []string, cfg joinConfig, printMu *sync.Mutex) (int, error) {
	codes := make([]int, len(taskNames))
	errs := make([]error, len(taskNames))

	var wg sync.WaitGroup
	for i, name := range taskNames {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			codes[i], errs[i] = streamTask(db, name, fmt.Sprintf("[%s] ", name), cfg, printMu)
		}(i, name)
	}
	wg.Wait()

	return aggregate(taskNames, codes, errs)
}

// joinGrouped drains tasks one at a time, wrapping each in a GitHub Actions
// collapsible ::group:: block. It waits for every task and returns the first
// failing task's exit code (non-zero if any failed).
func joinGrouped(db *sql.DB, taskNames []string, cfg joinConfig) (int, error) {
	codes := make([]int, len(taskNames))
	errs := make([]error, len(taskNames))

	var printMu sync.Mutex
	for i, name := range taskNames {
		fmt.Printf("::group::%s\n", name)
		codes[i], errs[i] = streamTask(db, name, "", cfg, &printMu)
		fmt.Println("::endgroup::")
	}

	return aggregate(taskNames, codes, errs)
}

// aggregate reduces per-task results to a single exit code: the first read
// error fails the join, otherwise the first non-zero exit code (in argument
// order), otherwise success.
func aggregate(taskNames []string, codes []int, errs []error) (int, error) {
	for i := range taskNames {
		if errs[i] != nil {
			return 1, errs[i]
		}
	}
	for i := range taskNames {
		if codes[i] != 0 {
			return codes[i], nil
		}
	}
	return 0, nil
}

// streamTask replays and tails one task's output to stdout/stderr and returns
// its exit code. Each line is written under printMu (so concurrently-joined
// tasks never interleave mid-line), prefixed with prefix and, when
// cfg.timestamps is set, the event's recorded time. It polls the database,
// advancing a monotonic id cursor, until it sees the exit event or the task
// stops emitting events for HeartbeatTimeout.
//
// Because it reads persisted events rather than a live process, joining a task
// that finished long ago replays its full history and exit code.
func streamTask(db *sql.DB, taskName, prefix string, cfg joinConfig, printMu *sync.Mutex) (int, error) {
	var lastID int64
	lastEventTime := time.Now()

	for {
		events, err := readEventsAfter(db, taskName, lastID)
		if err != nil {
			return 1, fmt.Errorf("failed to read events for %q: %w", taskName, err)
		}

		for _, e := range events {
			lastID = e.ID
			var w io.Writer
			switch e.Type {
			case EventTypeStdout:
				w = os.Stdout
			case EventTypeStderr:
				w = os.Stderr
			case EventTypeExit:
				return e.Code, nil
			default:
				continue
			}

			var b strings.Builder
			if cfg.timestamps {
				b.WriteString(formatTimestamp(e.Time))
			}
			b.WriteString(prefix)
			b.WriteString(e.Data)

			printMu.Lock()
			fmt.Fprint(w, b.String())
			printMu.Unlock()
		}

		if len(events) > 0 {
			lastEventTime = time.Now()
		} else if time.Since(lastEventTime) > HeartbeatTimeout {
			return 1, fmt.Errorf("heartbeat timeout: no events from task %q for %v", taskName, HeartbeatTimeout)
		}

		time.Sleep(JoinPollInterval)
	}
}

// formatTimestamp renders a stored RFC3339 event time as "HH:MM:SS.mmm ".
// If the stored value can't be parsed, it returns an empty string.
func formatTimestamp(stored string) string {
	t, err := time.Parse(time.RFC3339Nano, stored)
	if err != nil {
		return ""
	}
	return t.Format("15:04:05.000") + " "
}
