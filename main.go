package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	defaultBGXHome = "/tmp/bgx"
)

func getBGXHome() string {
	if home := os.Getenv("BGX_HOME"); home != "" {
		return home
	}
	return defaultBGXHome
}

func getLogPath(taskName string) string {
	return filepath.Join(getBGXHome(), taskName+".ndjson")
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "fork":
		if err := runFork(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "join":
		exitCode, err := runJoin(os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(exitCode)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `bgx - Background task executor with structured logging

Usage:
  bgx fork [--task-name NAME] -- COMMAND [ARGS...]
  bgx join [--task-name NAME]

Modes:
  Named task mode (detached):
    bgx fork --task-name task1 -- sleep 10
    bgx join --task-name task1

  Stdio mode (pipelined):
    bgx fork sleep 10 > task1.log
    tail -f task1.log | bgx join

Environment:
  BGX_HOME    Directory for log files (default: /tmp/bgx)

Configuration:
  Heartbeat interval: 5s
  Heartbeat timeout: 30s
`)
}
