package main

import "time"

// Event is a single record in a task's log. Each event is stored as one row
// in the SQLite `events` table.
type Event struct {
	Type string
	Time time.Time
	Data string

	// Start event fields
	PID     int
	Command []string

	// Exit event fields
	Code int

	// Heartbeat event fields
	CPUSeconds float64
	MemBytes   int64
}

const (
	EventTypeStart     = "start"
	EventTypeStdout    = "stdout"
	EventTypeStderr    = "stderr"
	EventTypeHeartbeat = "heartbeat"
	EventTypeExit      = "exit"
)

const (
	HeartbeatInterval = 5 * time.Second
	HeartbeatTimeout  = 30 * time.Second

	// JoinPollInterval is how often `join` polls the database for new events.
	JoinPollInterval = 100 * time.Millisecond
)
