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
- Linux: `bgx_Linux_x86_64.tar.gz` or `bgx_Linux_arm64.tar.gz`
- macOS: `bgx_Darwin_x86_64.tar.gz` or `bgx_Darwin_arm64.tar.gz`
- Windows: `bgx_Windows_x86_64.zip` or `bgx_Windows_arm64.zip`

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

### Example 3: Foreground with Recording (`exec`)
```bash
# Run in the foreground (output streams live, exits with the command's code),
# but capture the whole run to the database for later inspection.
bgx exec --task-name build -- make build

# Inspect afterwards (or upload $BGX_DB as a CI artifact).
sqlite3 "$BGX_DB" "SELECT type, data FROM events WHERE task='build' ORDER BY id"
```

### Example 4: Custom Database Location
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
