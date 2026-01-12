# bgx - BackGround eXecute

A lightweight tool for running commands in the background, designed for parallel execution in CI/CD pipelines like GitHub Actions.

```bash
# Start build something in background
bgx fork --task-name task1 -- make build

# Wait for the task to finish (stream stdout, stderr, and exit code)
bgx join --task-name build1
```

- When you run `bgx fork`, it creates a log file (e.g. `/tmp/bgx/build1.ndjson`) and start streaming the process output and exit code to that file.
- When you run `bgx join`, it will tail from that file and replay it.

## Installation

```bash
go install github.com/dtinth/bgx@latest
```

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
tail -f task1.log | bgx join
```

## Configuration

### Environment Variables

- **BGX_HOME**: Directory for log files (default: `/tmp/bgx`)

## Log Format

BGX uses NDJSON (newline-delimited JSON) for structured logging:

```json
{"type":"start","time":"2026-01-12T10:30:00Z","pid":12345,"command":["sleep","10"]}
{"type":"stdout","time":"2026-01-12T10:30:01Z","data":"Output line\n"}
{"type":"stderr","time":"2026-01-12T10:30:01Z","data":"Error line\n"}
{"type":"heartbeat","time":"2026-01-12T10:30:05Z","cpu_seconds":1.2,"mem_bytes":4096}
{"type":"exit","time":"2026-01-12T10:30:10Z","code":0}
```

## Limitations

- It only work on Linux right now (reads /proc)
- No built-in cleanup of old log files (manual cleanup needed)
