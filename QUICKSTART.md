# BGX Quick Start Guide

## Installation

### From Source
```bash
cd bgx

# Build
go build -o bgx

# Move to PATH (optional)
sudo mv bgx /usr/local/bin/
```

### Pre-built Binaries
Download from GitHub releases for your platform:
- Linux: `bgx-linux-amd64` or `bgx-linux-arm64`
- macOS: `bgx-darwin-amd64` or `bgx-darwin-arm64`

## Quick Examples

### Example 1: Basic Usage
```bash
# Fork a task
bgx fork --task-name mytask -- sleep 5

# Join (wait for it)
bgx join --task-name mytask
```

### Example 2: Parallel Builds
```bash
# Start multiple builds
bgx fork --task-name frontend -- npm run build
bgx fork --task-name backend -- go build ./...
bgx fork --task-name tests -- pytest

# Wait for all in one call (output tagged per task; fails if any failed)
bgx join --task-name frontend --task-name backend --task-name tests
```

### Example 3: Custom Database Location
```bash
export BGX_DB=/var/tmp/my-app.db
bgx fork --task-name deploy -- ./deploy.sh
bgx join --task-name deploy
```

## Common Patterns

### GitHub Actions Parallel Steps
```yaml
- name: Run tasks in parallel
  run: |
    bgx fork --task-name unit-tests -- npm test
    bgx fork --task-name e2e-tests -- npm run e2e
    bgx fork --task-name lint -- npm run lint

    # Wait for all in one call; the step fails if any task failed
    bgx join --task-name unit-tests --task-name e2e-tests --task-name lint
```

### Long-Running Deployment
```bash
# Start deployment
bgx fork --task-name prod-deploy -- ./deploy-prod.sh

# Do other work...
echo "Deployment started, doing other tasks..."

# Check status later
bgx join --task-name prod-deploy
echo "Deployment exit code: $?"
```

## Debugging

### Inspect the Database
```bash
# All events for a task
sqlite3 "$BGX_DB" "SELECT type, data FROM events WHERE task='mytask' ORDER BY id"

# Stdout only
sqlite3 "$BGX_DB" "SELECT data FROM events WHERE task='mytask' AND type='stdout'"

# Heartbeats (CPU/memory)
sqlite3 "$BGX_DB" "SELECT time, cpu_seconds, mem_bytes FROM events WHERE task='mytask' AND type='heartbeat'"
```

### Common Issues

**"task already exists"**
- Another task with the same name is already registered in the database.
- Use a different `--task-name`, or reset with `rm "$BGX_DB"`.

**"heartbeat timeout"**
- Process died without writing an exit event.
- Check system logs for crashes or verify the process isn't hanging.

**"task not found"**
- The task was never forked, or `BGX_DB` points at a different database than the one used by `bgx fork`.

## Next Steps

- Read [README.md](README.md) for full documentation
- See [CONTRIBUTING.md](CONTRIBUTING.md) for development guide
- Check examples in the test suite (bgx_test.go)
