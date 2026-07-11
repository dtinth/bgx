package main

import (
	"fmt"
	"os"
)

// Build information, set via -ldflags at release time by GoReleaser.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

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
	case "version", "--version", "-v":
		fmt.Printf("bgx %s (commit %s, built %s)\n", version, commit, date)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `bgx - Background task executor with structured logging

Usage:
  bgx fork --task-name NAME -- COMMAND [ARGS...]
  bgx join --task-name NAME [--task-name NAME ...]
  bgx version

Example:
  bgx fork --task-name build -- make build
  bgx fork --task-name test  -- make test
  bgx join --task-name build --task-name test

Tasks are recorded in a shared SQLite database, so independent processes
(for example, parallel steps in CI) can fork and join tasks concurrently.
Joining several tasks waits for all of them and fails if any did.

Environment:
  BGX_DB    Path to the shared database (default: <tmpdir>/bgx.db)

Configuration:
  Heartbeat interval: 5s
  Heartbeat timeout: 30s
`)
}
