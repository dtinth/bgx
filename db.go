package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS tasks (
	name       TEXT PRIMARY KEY,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	task        TEXT    NOT NULL,
	type        TEXT    NOT NULL,
	time        TEXT    NOT NULL,
	data        TEXT    NOT NULL DEFAULT '',
	pid         INTEGER NOT NULL DEFAULT 0,
	command     TEXT    NOT NULL DEFAULT '',
	code        INTEGER NOT NULL DEFAULT 0,
	cpu_seconds REAL    NOT NULL DEFAULT 0,
	mem_bytes   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_events_task_id ON events(task, id);
`

// getDBPath returns the path to the shared BGX database, honoring the BGX_DB
// environment variable and defaulting to <tmpdir>/bgx.db.
func getDBPath() string {
	if p := os.Getenv("BGX_DB"); p != "" {
		return p
	}
	return filepath.Join(os.TempDir(), "bgx.db")
}

// openDB opens (creating if needed) the shared database and ensures the schema
// exists. WAL mode plus a busy timeout lets independent `fork` daemons and
// `join` readers share one file concurrently. A single connection avoids
// self-contention on WAL's single-writer lock within a process.
func openDB() (*sql.DB, error) {
	path := getDBPath()
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create database directory: %w", err)
		}
	}

	// Percent-encode the path so that a BGX_DB containing '?', '#', or spaces
	// still forms a valid file: URI rather than being parsed as query/fragment.
	escaped := (&url.URL{Path: path}).EscapedPath()
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", escaped)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}
	return db, nil
}

// ErrTaskExists is returned by registerTask when the task name is already
// claimed. Callers wrap it with caller-appropriate guidance.
var ErrTaskExists = errors.New("task already exists")

// registerTask atomically claims a task name, returning ErrTaskExists if the
// name is already in use. INSERT OR IGNORE makes the claim race-free across
// concurrent forks: a UNIQUE collision affects zero rows instead of raising.
func registerTask(db *sql.DB, name string) error {
	res, err := db.Exec(
		"INSERT OR IGNORE INTO tasks(name, created_at) VALUES(?, ?)",
		name, time.Now().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("failed to register task: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to register task: %w", err)
	}
	if n == 0 {
		return ErrTaskExists
	}
	return nil
}

// unregisterTask releases a task name. It is best-effort, used to roll back a
// registration when the daemon fails to spawn.
func unregisterTask(db *sql.DB, name string) {
	_, _ = db.Exec("DELETE FROM tasks WHERE name = ?", name)
}

// taskExists reports whether a task with the given name has been registered.
func taskExists(db *sql.DB, name string) (bool, error) {
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM tasks WHERE name = ?", name).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// insertEvent appends one event row for the given task.
func insertEvent(db *sql.DB, task string, e Event) error {
	var command string
	if len(e.Command) > 0 {
		b, err := json.Marshal(e.Command)
		if err != nil {
			return err
		}
		command = string(b)
	}
	_, err := db.Exec(
		`INSERT INTO events(task, type, time, data, pid, command, code, cpu_seconds, mem_bytes)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task, e.Type, e.Time.Format(time.RFC3339Nano), e.Data,
		e.PID, command, e.Code, e.CPUSeconds, e.MemBytes,
	)
	return err
}

// eventRow is an event read back from the database. Only the fields consumed by
// `join` are decoded.
type eventRow struct {
	ID   int64
	Type string
	Time string
	Data string
	Code int
}

// readEventsAfter returns all events for a task with id greater than afterID,
// in insertion order. The monotonic id column acts as the read cursor.
func readEventsAfter(db *sql.DB, task string, afterID int64) ([]eventRow, error) {
	rows, err := db.Query(
		"SELECT id, type, time, data, code FROM events WHERE task = ? AND id > ? ORDER BY id",
		task, afterID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []eventRow
	for rows.Next() {
		var e eventRow
		if err := rows.Scan(&e.ID, &e.Type, &e.Time, &e.Data, &e.Code); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
