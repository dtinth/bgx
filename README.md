# BGX - Background Task Executor

A lightweight tool for running commands in the background with structured logging, designed for parallel execution in CI/CD pipelines like GitHub Actions.

## Features

- **Detached execution**: Fork processes to background without `nohup` or `&`
- **Structured logging**: NDJSON format with stdout, stderr, exit codes, and heartbeats
- **Two modes**: Named tasks (file-based) or stdio mode (pipe-based)
- **Process monitoring**: Automatic heartbeats (5s interval) with timeout detection (30s)
- **Exit code preservation**: Join returns the same exit code as the backgrounded process
- **Separated streams**: stdout and stderr are properly separated in output

## Installation

```bash
go build -o bgx
```

Or download a pre-built binary.

## Usage

### Named Task Mode (Detached)

Fork a task with a name:
```bash
bgx fork --task-name build1 -- make build
```

Output:
```
Started task 'build1' (log: /tmp/bgx/build1.ndjson)
To monitor: bgx join --task-name build1
```

Join (monitor) the task:
```bash
bgx join --task-name build1
```

This will:
- Stream stdout and stderr in real-time
- Wait for the process to complete
- Exit with the same exit code as the original command

### Stdio Mode (Pipelined)

Fork with output to file:
```bash
bgx fork sleep 10 > task1.log
```

Join by piping the log:
```bash
cat task1.log | bgx join
# or
tail -f task1.log | bgx join
```

## Configuration

### Environment Variables

- **BGX_HOME**: Directory for log files (default: `/tmp/bgx`)

Example:
```bash
export BGX_HOME=/var/log/bgx
bgx fork --task-name mytask -- ./script.sh
```

### Timing Constants

- Heartbeat interval: 5 seconds
- Heartbeat timeout: 30 seconds

## Log Format

BGX uses NDJSON (newline-delimited JSON) for structured logging:

```json
{"type":"start","time":"2026-01-12T10:30:00Z","pid":12345,"command":["sleep","10"]}
{"type":"stdout","time":"2026-01-12T10:30:01Z","data":"Output line\n"}
{"type":"stderr","time":"2026-01-12T10:30:01Z","data":"Error line\n"}
{"type":"heartbeat","time":"2026-01-12T10:30:05Z","cpu_seconds":1.2,"mem_bytes":4096}
{"type":"exit","time":"2026-01-12T10:30:10Z","code":0}
```

Event types:
- `start`: Process started (includes PID and command)
- `stdout`: Standard output line
- `stderr`: Standard error line
- `heartbeat`: Process health check (includes CPU and memory stats)
- `exit`: Process completed (includes exit code)

## Use Cases

### Parallel Builds in GitHub Actions

```bash
# Start multiple builds in parallel
bgx fork --task-name frontend -- npm run build
bgx fork --task-name backend -- cargo build --release
bgx fork --task-name tests -- pytest

# Wait for all to complete
bgx join --task-name frontend
bgx join --task-name backend
bgx join --task-name tests
```

### Long-Running Tasks

```bash
# Start a long task
bgx fork --task-name deploy -- ./deploy.sh

# Check on it later
bgx join --task-name deploy

# Or examine the log file directly
cat $BGX_HOME/deploy.ndjson | jq .
```

## Error Handling

### Duplicate Task Names

```bash
$ bgx fork --task-name build -- make
Started task 'build' (log: /tmp/bgx/build.ndjson)
To monitor: bgx join --task-name build

$ bgx fork --task-name build -- make
Error: log file already exists: /tmp/bgx/build.ndjson
Duplicate task name? Remove the file if this is intended.
```

### Heartbeat Timeout

If a process dies without writing an exit event, join will timeout:

```
Error: heartbeat timeout: no events received for 30s
```

## How It Works

1. **Fork** spawns a detached daemon process (double fork pattern)
2. The daemon monitors the command's stdout, stderr, and exit
3. Events are written to NDJSON log file in real-time
4. **Join** tails the log file and replays events
5. stdout/stderr are written to respective streams
6. Join exits with the original command's exit code

## Limitations

- Process stats (CPU/memory) currently only work on Linux (reads /proc)
- Heartbeat requires the monitoring process to keep running
- No built-in cleanup of old log files (manual cleanup needed)

## License

MIT

## Author

Built for handling parallel tasks in CI/CD pipelines where GitHub Actions' native parallelism is insufficient.
