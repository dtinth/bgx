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
- When you run `bgx exec`, it runs the command in the *foreground* (mirroring its output) while recording the same events — handy when you just want the log captured for later analysis.

Because every command shares one database file, independent processes (for example, parallel steps within a CI job) can fork and join tasks concurrently without juggling per-task log files.

Runs on Linux, macOS, and Windows. (CPU/memory heartbeats are Linux-only; everything else works everywhere.)

## Installation

### With mise (recommended)

Install directly from GitHub releases — mise auto-detects your OS/arch:

```bash
mise use -g github:dtinth/bgx
```

Pin a specific version:

```bash
mise use -g github:dtinth/bgx@0.2.0
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

Because output and exit code are persisted, you can `fork` a task, do other
work while it runs, and `join` it much later — even after it has finished. The
join replays the task's full output and exits with its recorded exit code; it
does not depend on the background process still being alive.

### Joining several tasks

Repeat `--task-name` to join multiple tasks in one call. `join` waits for all
of them, tags each output line with its task name, and exits non-zero if any
task failed (with the first failing task's exit code):

```bash
bgx join --task-name build --task-name test
```

```
[build] Compiling...
[test]  ok  	./...	0.42s
```

Two options control formatting:

- `--group` wraps each task's output in a [GitHub Actions collapsible
  group](https://docs.github.com/actions/reference/workflow-commands-for-github-actions#grouping-log-lines)
  (`::group::` / `::endgroup::`). Tasks are drained one at a time so each
  group stays contiguous, and the `[task]` line prefix is dropped since the
  group header already names the task.
- `--timestamps` prefixes each line with the event's recorded time
  (`HH:MM:SS.mmm`).

```bash
bgx join --group --task-name build --task-name test
```

```
::group::build
Compiling...
::endgroup::
::group::test
ok  	./...	0.42s
::endgroup::
```

### Recording a foreground command with `exec`

`bgx exec` runs a command in the foreground — you see its output live and it
exits with the command's exit code, exactly as if you had run the command
directly — but the full run (output, exit code, resource heartbeats) is also
recorded to the database. Nothing is detached; there is no separate `join`.

```bash
bgx exec --task-name build -- make build
```

This is useful for observability: run each step through `bgx exec`, then upload
the database as a build artifact and inspect every step's captured output and
timing after the fact.

```bash
sqlite3 "$BGX_DB" "SELECT task, type, data FROM events ORDER BY id"
```

## CI parallelization

The intended pattern: `fork` slow, independent setup work up front, keep doing
other work, then `join` each task right before you need its result. The join
both surfaces the output and gates on success, so a failed background task
fails the step.

```yaml
- name: Start background setup
  run: |
    export BGX_DB="$RUNNER_TEMP/bgx.db"
    bgx fork --task-name deps  -- npm ci
    bgx fork --task-name image -- docker pull ghcr.io/example/base:latest

- name: Build (runs concurrently with the setup above)
  run: |
    export BGX_DB="$RUNNER_TEMP/bgx.db"
    ./build.sh

- name: Wait for setup, fail if any failed
  run: |
    export BGX_DB="$RUNNER_TEMP/bgx.db"
    bgx join --task-name deps --task-name image
```

Set `BGX_DB` to a stable path shared by every step (as above) so `fork` and
`join` in different steps talk to the same database.

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

The workflow cross-compiles binaries (linux/darwin/windows × amd64/arm64),
packages them as `bgx_<Os>_<Arch>.tar.gz` archives (`.zip` on Windows) with a
`checksums.txt`, and publishes a GitHub Release. The archive names are chosen so
mise's `github:` backend can resolve the right asset automatically.

## Limitations

- Resource stats (CPU/memory heartbeats) only work on Linux (they read `/proc`); on macOS and Windows heartbeats are still emitted but carry zero stats.
- The shared database must live on a local filesystem — SQLite locking is unsafe over NFS, so parallel steps must share a machine, not just a database path.
- No built-in cleanup of old tasks (delete the database file, or rows, to reset).
