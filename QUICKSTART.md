# BGX Quick Start Guide

## Installation

### From Source
```bash
# Unzip the archive
unzip bgx.zip
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
- Windows: `bgx-windows-amd64.exe`

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

# Wait for all (in any order)
bgx join --task-name frontend
bgx join --task-name backend
bgx join --task-name tests
```

### Example 3: Stdio Mode
```bash
# Pipe output to file
bgx fork echo "Hello World" > output.log

# Process later
cat output.log | bgx join
```

### Example 4: Custom Log Location
```bash
export BGX_HOME=/var/log/my-app
bgx fork --task-name deploy -- ./deploy.sh
bgx join --task-name deploy
```

## Common Patterns

### GitHub Actions Parallel Jobs
```yaml
- name: Run tests in parallel
  run: |
    bgx fork --task-name unit-tests -- npm test
    bgx fork --task-name e2e-tests -- npm run e2e
    bgx fork --task-name lint -- npm run lint
    
    # Wait for all and check exit codes
    bgx join --task-name unit-tests
    bgx join --task-name e2e-tests
    bgx join --task-name lint
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

### Monitoring Output
```bash
# Start task
bgx fork --task-name watch-task -- ./long-script.sh

# Monitor in real-time from another terminal
tail -f /tmp/bgx/watch-task.ndjson | jq .

# Or join to see full output
bgx join --task-name watch-task
```

## Debugging

### View Log File
```bash
# Pretty print with jq
cat /tmp/bgx/mytask.ndjson | jq .

# Filter stdout only
cat /tmp/bgx/mytask.ndjson | jq 'select(.type=="stdout") | .data'

# View heartbeats
cat /tmp/bgx/mytask.ndjson | jq 'select(.type=="heartbeat")'
```

### Common Issues

**"log file already exists"**
- Another task with same name exists
- Remove old log: `rm /tmp/bgx/taskname.ndjson`

**"heartbeat timeout"**
- Process died without writing exit event
- Check system logs for crashes
- Verify process isn't hanging

**Join returns wrong exit code**
- Check log file for exit event
- Verify process completed
- Look for truncated log files

## Next Steps

- Read [README.md](README.md) for full documentation
- See [CONTRIBUTING.md](CONTRIBUTING.md) for development guide
- Check examples in the test suite (bgx_test.go)

## Getting Help

- GitHub Issues: Report bugs or request features
- Log Files: Always include log file contents when reporting issues
- Version: Run `bgx --version` (if implemented) or `go version` on build
