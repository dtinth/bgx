package main

import "time"

// Event types for NDJSON log format
type Event struct {
	Type string    `json:"type"`
	Time time.Time `json:"time"`
	Data string    `json:"data,omitempty"`
	
	// Start event fields
	PID     int      `json:"pid,omitempty"`
	Command []string `json:"command,omitempty"`
	
	// Exit event fields
	Code int `json:"code,omitempty"`
	
	// Heartbeat event fields
	CPUSeconds float64 `json:"cpu_seconds,omitempty"`
	MemBytes   int64   `json:"mem_bytes,omitempty"`
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
)
