package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNamedTaskMode(t *testing.T) {
	// Setup
	tmpDir := t.TempDir()
	os.Setenv("BGX_HOME", tmpDir)
	defer os.Unsetenv("BGX_HOME")
	
	bgxPath := "./bgx"
	taskName := "test_task"
	
	// Fork a simple command
	forkCmd := exec.Command(bgxPath, "fork", "--task-name", taskName, "--", "sh", "-c", "echo 'hello'; exit 42")
	output, err := forkCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Fork failed: %v, output: %s", err, output)
	}
	
	// Verify output mentions the task
	if !strings.Contains(string(output), taskName) {
		t.Errorf("Fork output should mention task name, got: %s", output)
	}
	
	// Wait a moment for process to complete
	time.Sleep(500 * time.Millisecond)
	
	// Join the task
	joinCmd := exec.Command(bgxPath, "join", "--task-name", taskName)
	joinOutput, err := joinCmd.CombinedOutput()
	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("Join failed: %v, output: %s", err, joinOutput)
	}
	
	// Verify exit code
	if exitCode != 42 {
		t.Errorf("Expected exit code 42, got %d", exitCode)
	}
	
	// Verify output
	if !strings.Contains(string(joinOutput), "hello") {
		t.Errorf("Expected 'hello' in output, got: %s", joinOutput)
	}
	
	// Verify log file exists and has correct structure
	logPath := filepath.Join(tmpDir, taskName+".ndjson")
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Errorf("Log file should exist at %s", logPath)
	}
}

func TestStdioMode(t *testing.T) {
	bgxPath := "./bgx"
	tmpLog := filepath.Join(t.TempDir(), "stdio_test.log")
	
	// Fork with output to file
	forkCmd := exec.Command(bgxPath, "fork", "echo", "stdio test")
	logFile, err := os.Create(tmpLog)
	if err != nil {
		t.Fatalf("Failed to create log file: %v", err)
	}
	forkCmd.Stdout = logFile
	forkCmd.Stderr = logFile
	
	if err := forkCmd.Run(); err != nil {
		logFile.Close()
		t.Fatalf("Fork failed: %v", err)
	}
	logFile.Close()
	
	// Wait for process to complete
	time.Sleep(500 * time.Millisecond)
	
	// Join by piping the log
	joinCmd := exec.Command(bgxPath, "join")
	logContent, err := os.ReadFile(tmpLog)
	if err != nil {
		t.Fatalf("Failed to read log: %v", err)
	}
	joinCmd.Stdin = strings.NewReader(string(logContent))
	
	joinOutput, err := joinCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Join failed: %v, output: %s", err, joinOutput)
	}
	
	// Verify output
	if !strings.Contains(string(joinOutput), "stdio test") {
		t.Errorf("Expected 'stdio test' in output, got: %s", joinOutput)
	}
}

func TestDuplicateTaskName(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("BGX_HOME", tmpDir)
	defer os.Unsetenv("BGX_HOME")
	
	bgxPath := "./bgx"
	taskName := "duplicate_test"
	
	// Fork first task
	forkCmd1 := exec.Command(bgxPath, "fork", "--task-name", taskName, "--", "echo", "first")
	if err := forkCmd1.Run(); err != nil {
		t.Fatalf("First fork failed: %v", err)
	}
	
	time.Sleep(100 * time.Millisecond)
	
	// Try to fork with same name - should fail
	forkCmd2 := exec.Command(bgxPath, "fork", "--task-name", taskName, "--", "echo", "second")
	output, err := forkCmd2.CombinedOutput()
	
	// Should fail
	if err == nil {
		t.Error("Second fork should have failed with duplicate name")
	}
	
	// Should mention duplicate or already exists
	outputStr := string(output)
	if !strings.Contains(outputStr, "already exists") && !strings.Contains(outputStr, "Duplicate") {
		t.Errorf("Error message should mention duplicate/already exists, got: %s", outputStr)
	}
}

func TestStdoutStderrSeparation(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("BGX_HOME", tmpDir)
	defer os.Unsetenv("BGX_HOME")
	
	bgxPath := "./bgx"
	taskName := "stderr_test"
	
	// Fork command that outputs to both stdout and stderr
	forkCmd := exec.Command(bgxPath, "fork", "--task-name", taskName, "--", "sh", "-c",
		"echo 'to stdout'; echo 'to stderr' >&2")
	if err := forkCmd.Run(); err != nil {
		t.Fatalf("Fork failed: %v", err)
	}
	
	time.Sleep(500 * time.Millisecond)
	
	// Join and capture stdout and stderr separately
	joinCmd := exec.Command(bgxPath, "join", "--task-name", taskName)
	var stdout, stderr strings.Builder
	joinCmd.Stdout = &stdout
	joinCmd.Stderr = &stderr
	
	if err := joinCmd.Run(); err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	
	// Verify separation
	if !strings.Contains(stdout.String(), "to stdout") {
		t.Errorf("stdout should contain 'to stdout', got: %s", stdout.String())
	}
	
	if !strings.Contains(stderr.String(), "to stderr") {
		t.Errorf("stderr should contain 'to stderr', got: %s", stderr.String())
	}
	
	// stdout should NOT contain stderr content
	if strings.Contains(stdout.String(), "to stderr") {
		t.Error("stdout should not contain stderr content")
	}
}

func TestLogFileStructure(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("BGX_HOME", tmpDir)
	defer os.Unsetenv("BGX_HOME")
	
	bgxPath := "./bgx"
	taskName := "log_structure_test"
	
	// Fork a command that runs long enough to generate heartbeats
	forkCmd := exec.Command(bgxPath, "fork", "--task-name", taskName, "--", "sh", "-c",
		"echo 'line1'; sleep 6; echo 'line2'; exit 7")
	if err := forkCmd.Run(); err != nil {
		t.Fatalf("Fork failed: %v", err)
	}
	
	// Wait for completion
	time.Sleep(7 * time.Second)
	
	// Read and parse log file
	logPath := filepath.Join(tmpDir, taskName+".ndjson")
	logFile, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("Failed to open log file: %v", err)
	}
	defer logFile.Close()
	
	scanner := bufio.NewScanner(logFile)
	var events []Event
	
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Errorf("Failed to parse event: %v, line: %s", err, scanner.Text())
			continue
		}
		events = append(events, event)
	}
	
	// Verify event sequence
	if len(events) == 0 {
		t.Fatal("No events found in log file")
	}
	
	// First event should be start
	if events[0].Type != EventTypeStart {
		t.Errorf("First event should be 'start', got: %s", events[0].Type)
	}
	
	// Last event should be exit
	if events[len(events)-1].Type != EventTypeExit {
		t.Errorf("Last event should be 'exit', got: %s", events[len(events)-1].Type)
	}
	
	// Should have exit code 7
	if events[len(events)-1].Code != 7 {
		t.Errorf("Exit code should be 7, got: %d", events[len(events)-1].Code)
	}
	
	// Should have at least one heartbeat (process ran for 6+ seconds)
	hasHeartbeat := false
	for _, event := range events {
		if event.Type == EventTypeHeartbeat {
			hasHeartbeat = true
			break
		}
	}
	if !hasHeartbeat {
		t.Error("Should have at least one heartbeat event")
	}
	
	// Should have stdout events
	hasStdout := false
	for _, event := range events {
		if event.Type == EventTypeStdout {
			hasStdout = true
			break
		}
	}
	if !hasStdout {
		t.Error("Should have stdout events")
	}
}

func TestParallelExecution(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("BGX_HOME", tmpDir)
	defer os.Unsetenv("BGX_HOME")
	
	bgxPath := "./bgx"
	numTasks := 3
	
	// Fork multiple tasks in parallel
	for i := 1; i <= numTasks; i++ {
		taskName := "parallel_" + string(rune('0'+i))
		forkCmd := exec.Command(bgxPath, "fork", "--task-name", taskName, "--", "sh", "-c",
			"sleep 1; echo 'done'")
		if err := forkCmd.Run(); err != nil {
			t.Fatalf("Fork task %d failed: %v", i, err)
		}
	}
	
	// Wait for all to complete
	time.Sleep(2 * time.Second)
	
	// Join all tasks
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
	tmpDir := t.TempDir()
	os.Setenv("BGX_HOME", tmpDir)
	defer os.Unsetenv("BGX_HOME")
	
	bgxPath := "./bgx"
	
	// Try to join non-existent task
	joinCmd := exec.Command(bgxPath, "join", "--task-name", "nonexistent")
	output, err := joinCmd.CombinedOutput()
	
	// Should fail
	if err == nil {
		t.Error("Join should fail for non-existent task")
	}
	
	// Should mention file not found or similar
	outputStr := string(output)
	if !strings.Contains(outputStr, "not exist") && !strings.Contains(outputStr, "not found") {
		t.Errorf("Error should mention file not existing, got: %s", outputStr)
	}
}

func TestLongRunningTask(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}
	
	tmpDir := t.TempDir()
	os.Setenv("BGX_HOME", tmpDir)
	defer os.Unsetenv("BGX_HOME")
	
	bgxPath := "./bgx"
	taskName := "long_running"
	
	// Fork a task that runs for 10 seconds
	forkCmd := exec.Command(bgxPath, "fork", "--task-name", taskName, "--", "sh", "-c",
		"for i in 1 2 3 4 5 6 7 8 9 10; do echo $i; sleep 1; done")
	if err := forkCmd.Run(); err != nil {
		t.Fatalf("Fork failed: %v", err)
	}
	
	// Start join immediately (while task is running)
	joinCmd := exec.Command(bgxPath, "join", "--task-name", taskName)
	output, err := joinCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Join failed: %v, output: %s", err, output)
	}
	
	// Should see all numbers
	outputStr := string(output)
	for i := 1; i <= 10; i++ {
		expected := fmt.Sprintf("%d", i)
		if !strings.Contains(outputStr, expected) {
			t.Errorf("Output should contain '%s', got: %s", expected, outputStr)
		}
	}
}

func TestEmptyCommand(t *testing.T) {
	bgxPath := "./bgx"
	
	// Try to fork without command
	forkCmd := exec.Command(bgxPath, "fork", "--task-name", "empty", "--")
	output, err := forkCmd.CombinedOutput()
	
	// Should fail
	if err == nil {
		t.Error("Fork should fail with empty command")
	}
	
	// Should mention no command
	if !strings.Contains(string(output), "no command") {
		t.Errorf("Error should mention no command, got: %s", output)
	}
}

func TestBGXHomeEnvironment(t *testing.T) {
	// Create custom BGX_HOME
	customHome := filepath.Join(t.TempDir(), "custom_bgx")
	os.Setenv("BGX_HOME", customHome)
	defer os.Unsetenv("BGX_HOME")
	
	bgxPath := "./bgx"
	taskName := "custom_home_test"
	
	// Fork task
	forkCmd := exec.Command(bgxPath, "fork", "--task-name", taskName, "--", "echo", "test")
	if err := forkCmd.Run(); err != nil {
		t.Fatalf("Fork failed: %v", err)
	}
	
	time.Sleep(500 * time.Millisecond)
	
	// Verify log file is in custom location
	expectedLogPath := filepath.Join(customHome, taskName+".ndjson")
	if _, err := os.Stat(expectedLogPath); os.IsNotExist(err) {
		t.Errorf("Log file should exist at custom location: %s", expectedLogPath)
	}
}
