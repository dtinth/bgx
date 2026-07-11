package main

import (
	"database/sql"
	"fmt"
	"os"
	"sync"
	"time"
)

// parseJoinArgs parses `join` arguments of the form:
//
//	--task-name NAME [--task-name NAME ...]
//
// Repeating --task-name joins several tasks at once.
func parseJoinArgs(args []string) ([]string, error) {
	var taskNames []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--task-name":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--task-name requires an argument")
			}
			taskNames = append(taskNames, args[i+1])
			i++
		default:
			return nil, fmt.Errorf("unexpected argument %q\nUsage: bgx join --task-name NAME [--task-name NAME ...]", args[i])
		}
	}
	if len(taskNames) == 0 {
		return nil, fmt.Errorf("--task-name is required")
	}
	return taskNames, nil
}

func runJoin(args []string) (int, error) {
	taskNames, err := parseJoinArgs(args)
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

	// A single task streams unprefixed, byte-for-byte. Multiple tasks stream
	// concurrently, each line tagged with its task name.
	var printMu sync.Mutex
	if len(taskNames) == 1 {
		return streamTask(db, taskNames[0], "", &printMu)
	}
	return joinAll(db, taskNames, &printMu)
}

// joinAll waits for every task, streaming each concurrently with a [task]
// prefix. It returns after all tasks finish, with a non-zero exit code if any
// task failed (the first failing task's code, in argument order).
func joinAll(db *sql.DB, taskNames []string, printMu *sync.Mutex) (int, error) {
	codes := make([]int, len(taskNames))
	errs := make([]error, len(taskNames))

	var wg sync.WaitGroup
	for i, name := range taskNames {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			codes[i], errs[i] = streamTask(db, name, fmt.Sprintf("[%s] ", name), printMu)
		}(i, name)
	}
	wg.Wait()

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
// tasks never interleave mid-line) and prefixed with prefix when non-empty. It
// polls the database, advancing a monotonic id cursor, until it sees the exit
// event or the task stops emitting events for HeartbeatTimeout.
//
// Because it reads persisted events rather than a live process, joining a task
// that finished long ago replays its full history and exit code.
func streamTask(db *sql.DB, taskName, prefix string, printMu *sync.Mutex) (int, error) {
	var lastID int64
	lastEventTime := time.Now()

	for {
		events, err := readEventsAfter(db, taskName, lastID)
		if err != nil {
			return 1, fmt.Errorf("failed to read events for %q: %w", taskName, err)
		}

		for _, e := range events {
			lastID = e.ID
			switch e.Type {
			case EventTypeStdout:
				printMu.Lock()
				fmt.Print(prefix + e.Data)
				printMu.Unlock()
			case EventTypeStderr:
				printMu.Lock()
				fmt.Fprint(os.Stderr, prefix+e.Data)
				printMu.Unlock()
			case EventTypeExit:
				return e.Code, nil
			}
		}

		if len(events) > 0 {
			lastEventTime = time.Now()
		} else if time.Since(lastEventTime) > HeartbeatTimeout {
			return 1, fmt.Errorf("heartbeat timeout: no events from task %q for %v", taskName, HeartbeatTimeout)
		}

		time.Sleep(JoinPollInterval)
	}
}
