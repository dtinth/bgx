package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

func runJoin(args []string) (int, error) {
	// Parse arguments
	var taskName string
	
	i := 0
	for i < len(args) {
		if args[i] == "--task-name" {
			if i+1 >= len(args) {
				return 1, fmt.Errorf("--task-name requires an argument")
			}
			taskName = args[i+1]
			i += 2
		} else {
			return 1, fmt.Errorf("unknown argument: %s", args[i])
		}
	}
	
	var reader io.Reader
	
	if taskName != "" {
		// Named task mode - read from file
		logPath := getLogPath(taskName)
		
		// Check if file exists
		if _, err := os.Stat(logPath); os.IsNotExist(err) {
			return 1, fmt.Errorf("log file does not exist: %s\nTask '%s' not found", logPath, taskName)
		}
		
		// Open file for reading
		f, err := os.Open(logPath)
		if err != nil {
			return 1, fmt.Errorf("failed to open log file: %w", err)
		}
		defer f.Close()
		
		reader = f
	} else {
		// Stdio mode - read from stdin
		reader = os.Stdin
	}
	
	return processEvents(reader)
}

func processEvents(reader io.Reader) (int, error) {
	// Determine if we're reading from a file or pipe
	var filePath string
	if f, ok := reader.(*os.File); ok {
		stat, err := f.Stat()
		if err == nil && stat.Mode().IsRegular() {
			// It's a regular file - get its path for reopening
			filePath = f.Name()
		}
	}
	
	var br *bufio.Reader
	if filePath != "" {
		// File mode - we'll reopen as needed
		br = nil
	} else {
		// Pipe/stdin mode
		br = bufio.NewReader(reader)
	}
	
	lastEventTime := time.Now()
	exitCode := 0
	hasExited := false
	offset := int64(0)
	
	for {
		// Check for timeout
		if time.Since(lastEventTime) > HeartbeatTimeout {
			if !hasExited {
				return 1, fmt.Errorf("heartbeat timeout: no events received for %v", HeartbeatTimeout)
			}
			break
		}
		
		var line string
		var err error
		
		if filePath != "" {
			// File tailing mode
			f, err := os.Open(filePath)
			if err != nil {
				return 1, fmt.Errorf("failed to open file: %w", err)
			}
			
			// Seek to our last position
			_, err = f.Seek(offset, 0)
			if err != nil {
				f.Close()
				return 1, fmt.Errorf("failed to seek: %w", err)
			}
			
			br = bufio.NewReader(f)
			line, err = br.ReadString('\n')
			
			if err == nil {
				// Successfully read a line
				offset += int64(len(line))
				f.Close()
			} else if err == io.EOF {
				// No more data yet
				f.Close()
				if hasExited {
					// Process has exited and we've reached EOF
					break
				}
				// Wait and retry
				time.Sleep(100 * time.Millisecond)
				continue
			} else {
				f.Close()
				return 1, fmt.Errorf("error reading file: %w", err)
			}
		} else {
			// Pipe/stdin mode
			line, err = br.ReadString('\n')
			if err == io.EOF {
				if hasExited {
					break
				}
				// For pipes, EOF means the writer closed
				return 1, fmt.Errorf("unexpected EOF before exit event")
			} else if err != nil {
				return 1, fmt.Errorf("error reading: %w", err)
			}
		}
		
		lastEventTime = time.Now()
		
		// Parse event
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to parse event: %v\n", err)
			continue
		}
		
		// Handle event based on type
		switch event.Type {
		case EventTypeStdout:
			fmt.Print(event.Data)
		case EventTypeStderr:
			fmt.Fprint(os.Stderr, event.Data)
		case EventTypeExit:
			exitCode = event.Code
			hasExited = true
		}
	}
	
	if !hasExited {
		return 1, fmt.Errorf("no exit event found")
	}
	
	return exitCode, nil
}
