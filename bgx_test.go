package main

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

const bgxPath = "./bgx"

// setupDB points BGX_DB at a fresh database for the duration of a test and
// returns its path.
func setupDB(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "bgx.db")
	t.Setenv("BGX_DB", dbPath)
	return dbPath
}

// readEvents loads all event types recorded for a task, in order.
func readEvents(t *testing.T, dbPath, taskName string) []Event {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(
		"SELECT type, data, code, cpu_seconds, mem_bytes FROM events WHERE task = ? ORDER BY id", taskName)
	if err != nil {
		t.Fatalf("Failed to query events: %v", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.Type, &e.Data, &e.Code, &e.CPUSeconds, &e.MemBytes); err != nil {
			t.Fatalf("Failed to scan event: %v", err)
		}
		events = append(events, e)
	}
	return events
}

func TestNamedTaskMode(t *testing.T) {
	setupDB(t)
	taskName := "test_task"

	forkCmd := exec.Command(bgxPath, "fork", "--task-name", taskName, "--", "sh", "-c", "echo 'hello'; exit 42")
	output, err := forkCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Fork failed: %v, output: %s", err, output)
	}
	if !strings.Contains(string(output), taskName) {
		t.Errorf("Fork output should mention task name, got: %s", output)
	}

	joinCmd := exec.Command(bgxPath, "join", "--task-name", taskName)
	joinOutput, err := joinCmd.CombinedOutput()
	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("Join failed: %v, output: %s", err, joinOutput)
	}

	if exitCode != 42 {
		t.Errorf("Expected exit code 42, got %d", exitCode)
	}
	if !strings.Contains(string(joinOutput), "hello") {
		t.Errorf("Expected 'hello' in output, got: %s", joinOutput)
	}
}

func TestDuplicateTaskName(t *testing.T) {
	setupDB(t)
	taskName := "duplicate_test"

	forkCmd1 := exec.Command(bgxPath, "fork", "--task-name", taskName, "--", "echo", "first")
	if err := forkCmd1.Run(); err != nil {
		t.Fatalf("First fork failed: %v", err)
	}

	forkCmd2 := exec.Command(bgxPath, "fork", "--task-name", taskName, "--", "echo", "second")
	output, err := forkCmd2.CombinedOutput()
	if err == nil {
		t.Error("Second fork should have failed with duplicate name")
	}
	if !strings.Contains(string(output), "already exists") {
		t.Errorf("Error message should mention already exists, got: %s", output)
	}
}

func TestStdoutStderrSeparation(t *testing.T) {
	setupDB(t)
	taskName := "stderr_test"

	forkCmd := exec.Command(bgxPath, "fork", "--task-name", taskName, "--", "sh", "-c",
		"echo 'to stdout'; echo 'to stderr' >&2")
	if err := forkCmd.Run(); err != nil {
		t.Fatalf("Fork failed: %v", err)
	}

	joinCmd := exec.Command(bgxPath, "join", "--task-name", taskName)
	var stdout, stderr strings.Builder
	joinCmd.Stdout = &stdout
	joinCmd.Stderr = &stderr
	if err := joinCmd.Run(); err != nil {
		t.Fatalf("Join failed: %v", err)
	}

	if !strings.Contains(stdout.String(), "to stdout") {
		t.Errorf("stdout should contain 'to stdout', got: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "to stderr") {
		t.Errorf("stderr should contain 'to stderr', got: %s", stderr.String())
	}
	if strings.Contains(stdout.String(), "to stderr") {
		t.Error("stdout should not contain stderr content")
	}
}

func TestEventStructure(t *testing.T) {
	dbPath := setupDB(t)
	taskName := "event_structure_test"

	forkCmd := exec.Command(bgxPath, "fork", "--task-name", taskName, "--", "sh", "-c",
		"echo 'line1'; sleep 6; echo 'line2'; exit 7")
	if err := forkCmd.Run(); err != nil {
		t.Fatalf("Fork failed: %v", err)
	}

	// Wait for the task to finish (it runs for 6+ seconds).
	joinCmd := exec.Command(bgxPath, "join", "--task-name", taskName)
	if _, err := joinCmd.CombinedOutput(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 7 {
			t.Fatalf("Join failed: %v", err)
		}
	}

	events := readEvents(t, dbPath, taskName)
	if len(events) == 0 {
		t.Fatal("No events found")
	}
	if events[0].Type != EventTypeStart {
		t.Errorf("First event should be 'start', got: %s", events[0].Type)
	}
	last := events[len(events)-1]
	if last.Type != EventTypeExit {
		t.Errorf("Last event should be 'exit', got: %s", last.Type)
	}
	if last.Code != 7 {
		t.Errorf("Exit code should be 7, got: %d", last.Code)
	}

	has := func(typ string) bool {
		for _, e := range events {
			if e.Type == typ {
				return true
			}
		}
		return false
	}
	if !has(EventTypeHeartbeat) {
		t.Error("Should have at least one heartbeat event")
	}
	if !has(EventTypeStdout) {
		t.Error("Should have stdout events")
	}

	// A running process should report non-zero resident memory in at least
	// one heartbeat (exercises the /proc/[pid]/statm parsing).
	sawMem := false
	for _, e := range events {
		if e.Type == EventTypeHeartbeat && e.MemBytes > 0 {
			sawMem = true
			break
		}
	}
	if !sawMem {
		t.Error("Expected at least one heartbeat with non-zero mem_bytes")
	}
}

func TestParallelExecution(t *testing.T) {
	setupDB(t)
	numTasks := 3

	for i := 1; i <= numTasks; i++ {
		taskName := "parallel_" + string(rune('0'+i))
		forkCmd := exec.Command(bgxPath, "fork", "--task-name", taskName, "--", "sh", "-c",
			"sleep 1; echo 'done'")
		if err := forkCmd.Run(); err != nil {
			t.Fatalf("Fork task %d failed: %v", i, err)
		}
	}

	for i := 1; i <= numTasks; i++ {
		taskName := "parallel_" + string(rune('0'+i))
		joinCmd := exec.Command(bgxPath, "join", "--task-name", taskName)
		output, err := joinCmd.CombinedOutput()
		if err != nil {
			t.Errorf("Join task %d failed: %v, output: %s", i, err, output)
		}
		if !strings.Contains(string(output), "done") {
			t.Errorf("Task %d output should contain 'done', got: %s", i, output)
		}
	}
}

func TestNonExistentTask(t *testing.T) {
	setupDB(t)

	joinCmd := exec.Command(bgxPath, "join", "--task-name", "nonexistent")
	output, err := joinCmd.CombinedOutput()
	if err == nil {
		t.Error("Join should fail for non-existent task")
	}
	if !strings.Contains(string(output), "not found") {
		t.Errorf("Error should mention task not found, got: %s", output)
	}
}

func TestFailedCommand(t *testing.T) {
	setupDB(t)
	taskName := "failed_command"

	// A command that cannot start should surface a clear error via join,
	// not hang until the heartbeat timeout.
	forkCmd := exec.Command(bgxPath, "fork", "--task-name", taskName, "--", "this-command-does-not-exist")
	if err := forkCmd.Run(); err != nil {
		t.Fatalf("Fork (parent) should succeed even if the command is invalid: %v", err)
	}

	start := time.Now()
	joinCmd := exec.Command(bgxPath, "join", "--task-name", taskName)
	output, err := joinCmd.CombinedOutput()
	if elapsed := time.Since(start); elapsed > HeartbeatTimeout {
		t.Errorf("Join took %v; a failed command should be reported promptly", elapsed)
	}
	if err == nil {
		t.Error("Join should report a non-zero exit for a command that failed to start")
	}
	if !strings.Contains(string(output), "bgx:") {
		t.Errorf("Output should include the startup error, got: %s", output)
	}
}

func TestLongRunningTask(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}
	setupDB(t)
	taskName := "long_running"

	forkCmd := exec.Command(bgxPath, "fork", "--task-name", taskName, "--", "sh", "-c",
		"for i in 1 2 3 4 5; do echo $i; sleep 1; done")
	if err := forkCmd.Run(); err != nil {
		t.Fatalf("Fork failed: %v", err)
	}

	// Start join immediately, while the task is still running.
	joinCmd := exec.Command(bgxPath, "join", "--task-name", taskName)
	output, err := joinCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Join failed: %v, output: %s", err, output)
	}
	for _, n := range []string{"1", "2", "3", "4", "5"} {
		if !strings.Contains(string(output), n) {
			t.Errorf("Output should contain '%s', got: %s", n, output)
		}
	}
}

func TestEmptyCommand(t *testing.T) {
	setupDB(t)

	forkCmd := exec.Command(bgxPath, "fork", "--task-name", "empty", "--")
	output, err := forkCmd.CombinedOutput()
	if err == nil {
		t.Error("Fork should fail with empty command")
	}
	if !strings.Contains(string(output), "no command") {
		t.Errorf("Error should mention no command, got: %s", output)
	}
}

func TestMissingTaskName(t *testing.T) {
	setupDB(t)

	forkCmd := exec.Command(bgxPath, "fork", "--", "echo", "hi")
	output, err := forkCmd.CombinedOutput()
	if err == nil {
		t.Error("Fork should fail without --task-name")
	}
	if !strings.Contains(string(output), "task-name is required") {
		t.Errorf("Error should mention required task-name, got: %s", output)
	}
}

func TestSuccessfulExitCode(t *testing.T) {
	setupDB(t)
	taskName := "success"

	forkCmd := exec.Command(bgxPath, "fork", "--task-name", taskName, "--", "sh", "-c", "echo ok")
	if err := forkCmd.Run(); err != nil {
		t.Fatalf("Fork failed: %v", err)
	}

	joinCmd := exec.Command(bgxPath, "join", "--task-name", taskName)
	output, err := joinCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Join should exit 0 for a successful command, got: %v, output: %s", err, output)
	}
	if !strings.Contains(string(output), "ok") {
		t.Errorf("Expected 'ok' in output, got: %s", output)
	}
}

// TestLongOutputLine guards against the 64KB bufio.Scanner line limit: a single
// line far larger than 64KB must be reproduced in full, with no trailing
// newline added.
func TestLongOutputLine(t *testing.T) {
	setupDB(t)
	taskName := "long_line"
	const n = 200000

	forkCmd := exec.Command(bgxPath, "fork", "--task-name", taskName, "--", "sh", "-c",
		fmt.Sprintf("head -c %d /dev/zero | tr '\\0' 'x'", n))
	if err := forkCmd.Run(); err != nil {
		t.Fatalf("Fork failed: %v", err)
	}

	joinCmd := exec.Command(bgxPath, "join", "--task-name", taskName)
	output, err := joinCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	if got := strings.Count(string(output), "x"); got != n {
		t.Errorf("Expected %d 'x' characters, got %d", n, got)
	}
}

// TestNoTrailingNewline verifies output without a final newline is preserved
// exactly (ReadString returns the trailing partial line on EOF).
func TestNoTrailingNewline(t *testing.T) {
	setupDB(t)
	taskName := "no_newline"

	forkCmd := exec.Command(bgxPath, "fork", "--task-name", taskName, "--", "printf", "no-newline")
	if err := forkCmd.Run(); err != nil {
		t.Fatalf("Fork failed: %v", err)
	}

	joinCmd := exec.Command(bgxPath, "join", "--task-name", taskName)
	var stdout strings.Builder
	joinCmd.Stdout = &stdout
	if err := joinCmd.Run(); err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	if stdout.String() != "no-newline" {
		t.Errorf("Expected exactly %q, got %q", "no-newline", stdout.String())
	}
}

// TestJoinAfterCompletion proves join replays a task's full output and exit
// code from the database even after the task has long since finished — the
// fork-early/join-late pattern that CI parallelization relies on.
func TestJoinAfterCompletion(t *testing.T) {
	setupDB(t)
	taskName := "done"

	forkCmd := exec.Command(bgxPath, "fork", "--task-name", taskName, "--", "sh", "-c", "echo finished; exit 5")
	if err := forkCmd.Run(); err != nil {
		t.Fatalf("Fork failed: %v", err)
	}

	// Let the task fully complete and the daemon exit before joining.
	time.Sleep(1 * time.Second)

	joinCmd := exec.Command(bgxPath, "join", "--task-name", taskName)
	output, err := joinCmd.CombinedOutput()
	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	if exitCode != 5 {
		t.Errorf("Expected replayed exit code 5, got %d", exitCode)
	}
	if !strings.Contains(string(output), "finished") {
		t.Errorf("Expected replayed output 'finished', got: %s", output)
	}
}

func TestMultiJoinSuccess(t *testing.T) {
	setupDB(t)

	for _, tn := range []string{"m1", "m2"} {
		forkCmd := exec.Command(bgxPath, "fork", "--task-name", tn, "--", "sh", "-c",
			fmt.Sprintf("echo out-%s", tn))
		if err := forkCmd.Run(); err != nil {
			t.Fatalf("Fork %s failed: %v", tn, err)
		}
	}

	joinCmd := exec.Command(bgxPath, "join", "--task-name", "m1", "--task-name", "m2")
	output, err := joinCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Join should succeed when all tasks pass, got: %v, output: %s", err, output)
	}
	if !strings.Contains(string(output), "[m1] out-m1") {
		t.Errorf("Expected prefixed '[m1] out-m1', got: %s", output)
	}
	if !strings.Contains(string(output), "[m2] out-m2") {
		t.Errorf("Expected prefixed '[m2] out-m2', got: %s", output)
	}
}

func TestMultiJoinFailurePropagates(t *testing.T) {
	setupDB(t)

	forkOK := exec.Command(bgxPath, "fork", "--task-name", "ok", "--", "sh", "-c", "echo good; exit 0")
	if err := forkOK.Run(); err != nil {
		t.Fatalf("Fork ok failed: %v", err)
	}
	forkBad := exec.Command(bgxPath, "fork", "--task-name", "bad", "--", "sh", "-c", "echo oops; exit 3")
	if err := forkBad.Run(); err != nil {
		t.Fatalf("Fork bad failed: %v", err)
	}

	joinCmd := exec.Command(bgxPath, "join", "--task-name", "ok", "--task-name", "bad")
	output, err := joinCmd.CombinedOutput()
	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("Join failed unexpectedly: %v", err)
	}
	if exitCode != 3 {
		t.Errorf("Expected exit code 3 from the failing task, got %d", exitCode)
	}
	// Both tasks' output should still be surfaced (wait-all, not fail-fast).
	if !strings.Contains(string(output), "good") || !strings.Contains(string(output), "oops") {
		t.Errorf("Expected output from both tasks, got: %s", output)
	}
}

func TestJoinGroup(t *testing.T) {
	setupDB(t)

	for _, tn := range []string{"g1", "g2"} {
		forkCmd := exec.Command(bgxPath, "fork", "--task-name", tn, "--", "echo", "in-"+tn)
		if err := forkCmd.Run(); err != nil {
			t.Fatalf("Fork %s failed: %v", tn, err)
		}
	}

	joinCmd := exec.Command(bgxPath, "join", "--group", "--task-name", "g1", "--task-name", "g2")
	output, err := joinCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Join failed: %v, output: %s", err, output)
	}
	out := string(output)

	// Each task's output should be wrapped in its own contiguous group block.
	for _, want := range []string{"::group::g1", "::group::g2", "::endgroup::"} {
		if !strings.Contains(out, want) {
			t.Errorf("Expected %q in grouped output, got: %s", want, out)
		}
	}
	// g1's group header must come before g1's content, before g2's header.
	iG1 := strings.Index(out, "::group::g1")
	iContent := strings.Index(out, "in-g1")
	iG2 := strings.Index(out, "::group::g2")
	if !(iG1 < iContent && iContent < iG2) {
		t.Errorf("Group ordering wrong (g1=%d content=%d g2=%d): %s", iG1, iContent, iG2, out)
	}
	// Group mode does not add the [task] line prefix (the group names it).
	if strings.Contains(out, "[g1]") {
		t.Errorf("Group mode should not add [task] prefixes, got: %s", out)
	}
}

func TestJoinTimestamps(t *testing.T) {
	setupDB(t)
	taskName := "ts"

	forkCmd := exec.Command(bgxPath, "fork", "--task-name", taskName, "--", "echo", "hello")
	if err := forkCmd.Run(); err != nil {
		t.Fatalf("Fork failed: %v", err)
	}

	joinCmd := exec.Command(bgxPath, "join", "--timestamps", "--task-name", taskName)
	var stdout strings.Builder
	joinCmd.Stdout = &stdout
	if err := joinCmd.Run(); err != nil {
		t.Fatalf("Join failed: %v", err)
	}

	// Expect a leading HH:MM:SS.mmm timestamp before the output line.
	tsLine := regexp.MustCompile(`^\d{2}:\d{2}:\d{2}\.\d{3} hello`)
	if !tsLine.MatchString(strings.TrimSpace(stdout.String())) {
		t.Errorf("Expected a timestamp-prefixed line, got: %q", stdout.String())
	}
}

// TestExecForeground verifies `bgx exec` runs the command in the foreground,
// mirrors its output to the terminal, records the lifecycle to the database,
// and exits with the command's exit code.
func TestExecForeground(t *testing.T) {
	dbPath := setupDB(t)
	taskName := "exec_fg"

	execCmd := exec.Command(bgxPath, "exec", "--task-name", taskName, "--", "sh", "-c",
		"echo out-line; echo err-line >&2; exit 9")
	var stdout, stderr strings.Builder
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr
	err := execCmd.Run()

	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("Exec failed unexpectedly: %v", err)
	}
	if exitCode != 9 {
		t.Errorf("Expected exec to exit with the command's code 9, got %d", exitCode)
	}

	// Output is mirrored live to the terminal, on the right streams.
	if !strings.Contains(stdout.String(), "out-line") {
		t.Errorf("Expected 'out-line' mirrored to stdout, got: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "err-line") {
		t.Errorf("Expected 'err-line' mirrored to stderr, got: %q", stderr.String())
	}

	// The run is also persisted for later inspection (the observability point).
	events := readEvents(t, dbPath, taskName)
	if len(events) == 0 {
		t.Fatal("Exec should record events")
	}
	if events[0].Type != EventTypeStart {
		t.Errorf("First event should be 'start', got: %s", events[0].Type)
	}
	last := events[len(events)-1]
	if last.Type != EventTypeExit || last.Code != 9 {
		t.Errorf("Last event should be exit with code 9, got: %s code %d", last.Type, last.Code)
	}
	sawStdout, sawStderr := false, false
	for _, e := range events {
		switch e.Type {
		case EventTypeStdout:
			sawStdout = true
		case EventTypeStderr:
			sawStderr = true
		}
	}
	if !sawStdout || !sawStderr {
		t.Errorf("Exec should record both stdout and stderr events (stdout=%v stderr=%v)", sawStdout, sawStderr)
	}
}

// TestExecThenJoin verifies a task run via exec can be joined afterwards,
// replaying its recorded output and exit code just like a forked task.
func TestExecThenJoin(t *testing.T) {
	setupDB(t)
	taskName := "exec_join"

	execCmd := exec.Command(bgxPath, "exec", "--task-name", taskName, "--", "sh", "-c", "echo recorded; exit 4")
	if _, err := execCmd.CombinedOutput(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 4 {
			t.Fatalf("Exec failed unexpectedly: %v", err)
		}
	}

	joinCmd := exec.Command(bgxPath, "join", "--task-name", taskName)
	output, err := joinCmd.CombinedOutput()
	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	if exitCode != 4 {
		t.Errorf("Expected replayed exit code 4, got %d", exitCode)
	}
	if !strings.Contains(string(output), "recorded") {
		t.Errorf("Expected replayed output 'recorded', got: %s", output)
	}
}

// TestExecDuplicateName verifies exec claims the task name like fork does, so a
// name already in use is rejected rather than silently appended to.
func TestExecDuplicateName(t *testing.T) {
	setupDB(t)
	taskName := "exec_dup"

	first := exec.Command(bgxPath, "exec", "--task-name", taskName, "--", "echo", "first")
	if err := first.Run(); err != nil {
		t.Fatalf("First exec failed: %v", err)
	}

	second := exec.Command(bgxPath, "exec", "--task-name", taskName, "--", "echo", "second")
	output, err := second.CombinedOutput()
	if err == nil {
		t.Error("Second exec with a duplicate name should fail")
	}
	if !strings.Contains(string(output), "already exists") {
		t.Errorf("Error should mention already exists, got: %s", output)
	}
}

func TestDaemonModeNotLeaked(t *testing.T) {
	setupDB(t)
	taskName := "env_leak"

	forkCmd := exec.Command(bgxPath, "fork", "--task-name", taskName, "--", "sh", "-c",
		`echo "mode=[$BGX_DAEMON_MODE]"`)
	if err := forkCmd.Run(); err != nil {
		t.Fatalf("Fork failed: %v", err)
	}

	joinCmd := exec.Command(bgxPath, "join", "--task-name", taskName)
	output, err := joinCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	if !strings.Contains(string(output), "mode=[]") {
		t.Errorf("BGX_DAEMON_MODE should not leak into the task, got: %s", output)
	}
}

func TestBGXDBWithSpaces(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "dir with spaces", "bgx.db")
	t.Setenv("BGX_DB", dbPath)
	taskName := "spaces"

	forkCmd := exec.Command(bgxPath, "fork", "--task-name", taskName, "--", "echo", "hi")
	if err := forkCmd.Run(); err != nil {
		t.Fatalf("Fork failed: %v", err)
	}

	joinCmd := exec.Command(bgxPath, "join", "--task-name", taskName)
	output, err := joinCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Join failed: %v, output: %s", err, output)
	}
	if !strings.Contains(string(output), "hi") {
		t.Errorf("Expected 'hi' in output, got: %s", output)
	}
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Errorf("Database should exist at path with spaces: %s", dbPath)
	}
}

func TestBGXDBEnvironment(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "custom", "mydb.db")
	t.Setenv("BGX_DB", dbPath)
	taskName := "custom_db_test"

	forkCmd := exec.Command(bgxPath, "fork", "--task-name", taskName, "--", "echo", "test")
	if err := forkCmd.Run(); err != nil {
		t.Fatalf("Fork failed: %v", err)
	}

	joinCmd := exec.Command(bgxPath, "join", "--task-name", taskName)
	if _, err := joinCmd.CombinedOutput(); err != nil {
		t.Fatalf("Join failed: %v", err)
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Errorf("Database should exist at custom location: %s", dbPath)
	}
}
