package main

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// parseForkArgs parses `fork` arguments of the form:
//
//	--task-name NAME -- COMMAND [ARGS...]
func parseForkArgs(args []string) (taskName string, command []string, err error) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--task-name":
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("--task-name requires an argument")
			}
			taskName = args[i+1]
			i++
		case "--":
			command = args[i+1:]
			i = len(args)
		default:
			return "", nil, fmt.Errorf("unexpected argument %q\nUsage: bgx fork --task-name NAME -- COMMAND [ARGS...]", args[i])
		}
	}
	if taskName == "" {
		return "", nil, fmt.Errorf("--task-name is required")
	}
	if len(command) == 0 {
		return "", nil, fmt.Errorf("no command specified")
	}
	return taskName, command, nil
}

func runFork(args []string) error {
	taskName, command, err := parseForkArgs(args)
	if err != nil {
		return err
	}

	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	// Daemon mode: we are the detached child; actually run the command.
	if os.Getenv("BGX_DAEMON_MODE") == "1" {
		_, err := executeProcess(db, taskName, command, false)
		return err
	}

	// Parent mode: atomically claim the task name, then spawn the daemon.
	if err := registerTask(db, taskName); err != nil {
		if errors.Is(err, ErrTaskExists) {
			return fmt.Errorf("task %q already exists (BGX_DB=%s)\nUse a different --task-name or remove the database.", taskName, getDBPath())
		}
		return err
	}

	env := append(os.Environ(), "BGX_DAEMON_MODE=1")
	daemonArgs := []string{"fork", "--task-name", taskName, "--"}
	daemonArgs = append(daemonArgs, command...)

	cmd := exec.Command(os.Args[0], daemonArgs...)
	cmd.Env = env
	cmd.SysProcAttr = daemonSysProcAttr() // detach so the daemon outlives this step

	if err := cmd.Start(); err != nil {
		unregisterTask(db, taskName) // release the name; nothing ran
		return fmt.Errorf("failed to start daemon: %w", err)
	}
	cmd.Process.Release()

	fmt.Fprintf(os.Stderr, "Started task '%s' (BGX_DB: %s)\n", taskName, getDBPath())
	fmt.Fprintf(os.Stderr, "To monitor: bgx join --task-name %s\n", taskName)
	return nil
}

// executeProcess launches the command and records its lifecycle as events,
// returning the command's exit code. When mirror is true, stdout and stderr are
// also written live to the terminal (used by `bgx exec`, which runs in the
// foreground); otherwise output is only persisted (used by the `fork` daemon).
func executeProcess(db *sql.DB, taskName string, command []string, mirror bool) (int, error) {
	cmd := exec.Command(command[0], command[1:]...)
	// Don't leak bgx's internal daemon flag into the task; otherwise a nested
	// `bgx fork` inside the task would think it is a daemon and not detach.
	cmd.Env = environWithout("BGX_DAEMON_MODE")

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return recordStartupFailure(db, taskName, fmt.Errorf("failed to create stdout pipe: %w", err))
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return recordStartupFailure(db, taskName, fmt.Errorf("failed to create stderr pipe: %w", err))
	}

	if err := cmd.Start(); err != nil {
		return recordStartupFailure(db, taskName, fmt.Errorf("failed to start command: %w", err))
	}

	pid := cmd.Process.Pid
	writeEvent(db, taskName, Event{
		Type:    EventTypeStart,
		Time:    time.Now(),
		PID:     pid,
		Command: command,
	})

	return runProcess(db, taskName, cmd, stdoutPipe, stderrPipe, pid, mirror)
}

// recordStartupFailure writes a stderr + exit event so that a `join` waiting on
// this task fails fast with a clear message instead of hitting a heartbeat
// timeout. Exit code 127 mirrors the shell's "command not found".
func recordStartupFailure(db *sql.DB, taskName string, cause error) (int, error) {
	writeEvent(db, taskName, Event{
		Type: EventTypeStderr,
		Time: time.Now(),
		Data: fmt.Sprintf("bgx: %v\n", cause),
	})
	writeEvent(db, taskName, Event{
		Type: EventTypeExit,
		Time: time.Now(),
		Code: 127,
	})
	return 127, cause
}

func runProcess(db *sql.DB, taskName string, cmd *exec.Cmd, stdoutPipe, stderrPipe io.ReadCloser, pid int, mirror bool) (int, error) {
	streamOutput := func(pipe io.ReadCloser, eventType string, tee io.Writer) {
		br := bufio.NewReader(pipe)
		for {
			// ReadString has no line-length limit, so arbitrarily long
			// output lines are preserved intact.
			line, err := br.ReadString('\n')
			if len(line) > 0 {
				if tee != nil {
					io.WriteString(tee, line)
				}
				writeEvent(db, taskName, Event{
					Type: eventType,
					Time: time.Now(),
					Data: line,
				})
			}
			if err != nil {
				return
			}
		}
	}

	var stdoutTee, stderrTee io.Writer
	if mirror {
		stdoutTee, stderrTee = os.Stdout, os.Stderr
	}

	// Read both pipes to EOF before calling cmd.Wait: Wait closes the pipes,
	// so calling it while reads are in flight would truncate output.
	var readers sync.WaitGroup
	readers.Add(2)
	go func() { defer readers.Done(); streamOutput(stdoutPipe, EventTypeStdout, stdoutTee) }()
	go func() { defer readers.Done(); streamOutput(stderrPipe, EventTypeStderr, stderrTee) }()

	// Emit heartbeats until the process is reaped (see close(done) below).
	done := make(chan struct{})
	var heartbeat sync.WaitGroup
	heartbeat.Add(1)
	go func() {
		defer heartbeat.Done()
		ticker := time.NewTicker(HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				cpuTime, memBytes := getProcessStats(pid)
				writeEvent(db, taskName, Event{
					Type:       EventTypeHeartbeat,
					Time:       time.Now(),
					CPUSeconds: cpuTime,
					MemBytes:   memBytes,
				})
			case <-done:
				return
			}
		}
	}()

	// Drain both pipes (readers hit EOF when the process closes its output),
	// then reap the process. Heartbeats keep flowing until cmd.Wait returns,
	// so a task that closes stdout/stderr but keeps running is still reported
	// alive rather than tripping join's heartbeat timeout.
	readers.Wait()
	err := cmd.Wait()
	close(done)
	heartbeat.Wait()

	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			exitCode = 1
		}
	}

	writeEvent(db, taskName, Event{
		Type: EventTypeExit,
		Time: time.Now(),
		Code: exitCode,
	})
	return exitCode, nil
}

// environWithout returns a copy of the current environment with any assignment
// of the given key removed.
func environWithout(key string) []string {
	prefix := key + "="
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, prefix) {
			out = append(out, kv)
		}
	}
	return out
}

// writeEvent records an event, reporting (rather than silently dropping)
// failures. A single database connection serializes concurrent writers.
func writeEvent(db *sql.DB, taskName string, e Event) {
	if err := insertEvent(db, taskName, e); err != nil {
		fmt.Fprintf(os.Stderr, "bgx: failed to record %s event: %v\n", e.Type, err)
	}
}
