# bgx - BackGround eXecute

A lightweight tool for running commands in the background, designed for parallel execution in CI/CD pipelines like GitHub Actions.

```bash
# Start building something in the background
bgx fork --task-name build -- make build

# Wait for the task to finish (stream stdout, stderr, and exit code)
bgx join --task-name build
```

- When you run `bgx fork`, it detaches the command into the background and records its output, resource usage, and exit code as events in a shared SQLite database.
- When you run `bgx join`, it replays those events from the database — streaming stdout/stderr live and exiting with the command's exit code.

Because every command shares one database file, independent processes (for example, parallel steps within a CI job) can fork and join tasks concurrently without juggling per-task log files.

## Installation

### With mise (recommended)

Install directly from GitHub releases — mise auto-detects your OS/arch:

```bash
mise use -g github:dtinth/bgx
```

Pin a specific version:

```bash
mise use -g github:dtinth/bgx@1.0.0
```

### With go install

```bash
go install github.com/dtinth/bgx@latest
```

### Manual download

Grab a prebuilt archive from the [releases page](https://github.com/dtinth/bgx/releases), extract it, and put the `bgx` binary on your `PATH`.

## Usage

Fork a task with a name:
```bash
bgx fork --task-name build -- make build
```

Output:
```
Started task 'build' (BGX_DB: /tmp/bgx.db)
To monitor: bgx join --task-name build
```

Join (monitor) the task:
```bash
bgx join --task-name build
```

This will:
- Stream stdout and stderr in real-time
- Wait for the process to complete
- Exit with the same exit code as the original command

## Configuration

### Environment Variables

- **BGX_DB**: Path to the shared SQLite database (default: `<tmpdir>/bgx.db`, e.g. `/tmp/bgx.db`)

## Storage Format

BGX records each task's lifecycle as rows in an `events` table:

| column      | description                                    |
|-------------|------------------------------------------------|
| id          | monotonic event id (used as the read cursor)   |
| task        | task name                                      |
| type        | `start`, `stdout`, `stderr`, `heartbeat`, `exit` |
| time        | RFC3339 timestamp                              |
| data        | output line (for stdout/stderr)                |
| pid         | process id (start event)                       |
| command     | JSON-encoded command (start event)             |
| code        | exit code (exit event)                         |
| cpu_seconds | cumulative CPU time (heartbeat event)          |
| mem_bytes   | resident memory (heartbeat event)              |

Inspect a task directly with the `sqlite3` CLI:

```bash
sqlite3 "$BGX_DB" "SELECT type, data FROM events WHERE task='build' ORDER BY id"
```

## Releasing

Releases are automated with [GoReleaser](https://goreleaser.com) via
`.github/workflows/release.yml`. To cut a release, push a semver tag:

```bash
git tag v1.0.0
git push origin v1.0.0
```

The workflow cross-compiles binaries (linux/darwin × amd64/arm64), packages
them as `bgx_<Os>_<Arch>.tar.gz` archives with a `checksums.txt`, and publishes
a GitHub Release. The archive names are chosen so mise's `github:` backend can
resolve the right asset automatically.

## Limitations

- Resource stats (CPU/memory heartbeats) only work on Linux (they read `/proc`).
- The shared database must live on a local filesystem — SQLite locking is unsafe over NFS, so parallel steps must share a machine, not just a database path.
- No built-in cleanup of old tasks (delete the database file, or rows, to reset).
