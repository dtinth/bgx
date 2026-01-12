package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

func runFork(args []string) error {
	// Parse arguments
	var taskName string
	var command []string

	i := 0
	for i < len(args) {
		if args[i] == "--task-name" {
			if i+1 >= len(args) {
				return fmt.Errorf("--task-name requires an argument")
			}
			taskName = args[i+1]
			i += 2
		} else if args[i] == "--" {
			command = args[i+1:]
			break
		} else {
			// Stdio mode - all args are the command
			command = args[i:]
			break
		}
	}

	if len(command) == 0 {
		return fmt.Errorf("no command specified")
	}

	// Determine mode
	if taskName != "" {
		// Named task mode
		bgxHome := getBGXHome()
		if err := os.MkdirAll(bgxHome, 0755); err != nil {
			return fmt.Errorf("failed to create BGX_HOME: %w", err)
		}
		
		logPath := getLogPath(taskName)
		
		// Check if log file already exists
		if _, err := os.Stat(logPath); err == nil {
			return fmt.Errorf("log file already exists: %s\nDuplicate task name? Remove the file if this is intended.", logPath)
		}
		
		// Check if we're being called as the daemon (internal mode)
		if os.Getenv("BGX_DAEMON_MODE") == "1" {
			// We're in daemon mode - actually run the process
			f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				return fmt.Errorf("failed to create log file: %w", err)
			}
			defer f.Close()
			
			return executeProcess(command, f)
		}
		
		// Not in daemon mode - fork ourselves into background
		// Re-execute bgx with BGX_DAEMON_MODE=1
		env := append(os.Environ(), "BGX_DAEMON_MODE=1")
		
		// Build the args for the daemon process
		daemonArgs := []string{os.Args[0], "fork", "--task-name", taskName, "--"}
		daemonArgs = append(daemonArgs, command...)
		
		cmd := exec.Command(daemonArgs[0], daemonArgs[1:]...)
		cmd.Env = env
		
		// Detach from terminal
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setsid: true,
		}
		
		// Start the daemon
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start daemon: %w", err)
		}
		
		// Release the process (don't wait)
		cmd.Process.Release()
		
		// Print helpful output
		fmt.Fprintf(os.Stderr, "Started task '%s' (log: %s)\n", taskName, logPath)
		fmt.Fprintf(os.Stderr, "To monitor: bgx join --task-name %s\n", taskName)
		
	} else {
		// Stdio mode - write to stdout, run in foreground
		return executeProcess(command, os.Stdout)
	}
	
	return nil
}

func executeProcess(command []string, writer io.Writer) error {
	// Create the command
	cmd := exec.Command(command[0], command[1:]...)
	
	// Get pipes for stdout and stderr
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}
	
	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}
	
	pid := cmd.Process.Pid
	
	// Write start event
	encoder := json.NewEncoder(writer)
	if err := encoder.Encode(Event{
		Type:    EventTypeStart,
		Time:    time.Now(),
		PID:     pid,
		Command: command,
	}); err != nil {
		return fmt.Errorf("failed to write start event: %w", err)
	}
	
	return runProcess(cmd, stdoutPipe, stderrPipe, writer, pid)
}

func runProcess(cmd *exec.Cmd, stdoutPipe, stderrPipe io.ReadCloser, writer io.Writer, pid int) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	encoder := json.NewEncoder(writer)
	
	// Helper to write events safely
	writeEvent := func(event Event) {
		mu.Lock()
		defer mu.Unlock()
		encoder.Encode(event)
	}
	
	// Stream stdout
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			line := scanner.Text() + "\n"
			writeEvent(Event{
				Type: EventTypeStdout,
				Time: time.Now(),
				Data: line,
			})
		}
	}()
	
	// Stream stderr
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			line := scanner.Text() + "\n"
			writeEvent(Event{
				Type: EventTypeStderr,
				Time: time.Now(),
				Data: line,
			})
		}
	}()
	
	// Heartbeat generator
	done := make(chan bool)
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(HeartbeatInterval)
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				cpuTime, memBytes := getProcessStats(pid)
				writeEvent(Event{
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
	
	// Wait for process to complete
	err := cmd.Wait()
	close(done) // Stop heartbeat
	
	// Wait for all goroutines to finish reading
	wg.Wait()
	
	// Get exit code
	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			exitCode = 1
		}
	}
	
	// Write exit event
	writeEvent(Event{
		Type: EventTypeExit,
		Time: time.Now(),
		Code: exitCode,
	})
	
	return nil
}

func getProcessStats(pid int) (cpuSeconds float64, memBytes int64) {
	// Try to read /proc/[pid]/stat for CPU time
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	data, err := os.ReadFile(statPath)
	if err != nil {
		return 0, 0
	}
	
	// Parse stat file - CPU times are fields 14 and 15 (utime and stime)
	// This is a simplified parser
	var comm string
	var utime, stime uint64
	fmt.Sscanf(string(data), "%d %s %*c %*d %*d %*d %*d %*d %*d %*d %*d %*d %*d %d %d",
		&pid, &comm, &utime, &stime)
	
	// Convert clock ticks to seconds (usually 100 ticks per second)
	clockTicks := float64(100) // syscall.CLK_TCK on most systems
	cpuSeconds = float64(utime+stime) / clockTicks
	
	// Try to get RSS from statm (simpler than parsing status)
	statmPath := fmt.Sprintf("/proc/%d/statm", pid)
	statmData, err := os.ReadFile(statmPath)
	if err != nil {
		return cpuSeconds, 0
	}
	
	var size, resident uint64
	fmt.Sscanf(string(statmData), "%d %d", &size, &resident)
	
	// Convert pages to bytes (usually 4096 bytes per page)
	pageSize := int64(syscall.Getpagesize())
	memBytes = int64(resident) * pageSize
	
	return cpuSeconds, memBytes
}
