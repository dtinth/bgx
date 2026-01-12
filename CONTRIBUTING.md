# Contributing to BGX

Thanks for your interest in contributing to BGX!

## Development Setup

1. Install Go 1.22 or later
2. Clone the repository
3. Build: `go build -o bgx`
4. Run tests: `go test -v ./...`

## Running Tests

```bash
# Run all tests (includes long-running tests)
go test -v -timeout 30m ./...

# Run only fast tests
go test -v -short -timeout 5m ./...

# Run specific test
go test -v -run TestNamedTaskMode

# Run with coverage
go test -v -cover ./...
```

## Code Structure

- `main.go` - CLI entry point and command routing
- `types.go` - Event types and constants
- `fork.go` - Process forking and monitoring
- `join.go` - Log tailing and output replication
- `bgx_test.go` - Acceptance tests

## Adding New Features

1. Add tests first (TDD approach)
2. Implement the feature
3. Update README.md if needed
4. Run full test suite
5. Submit PR

## Testing Guidelines

- Write acceptance tests for user-facing behavior
- Test both named task and stdio modes
- Include error cases
- Use `t.TempDir()` for test isolation
- Clean up resources with `defer`

## Code Style

- Follow standard Go conventions
- Run `go fmt` before committing
- Keep functions focused and small
- Comment exported functions

## Submitting Changes

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Run tests
5. Submit a pull request

## Reporting Issues

- Describe the expected behavior
- Describe the actual behavior
- Include steps to reproduce
- Include BGX version and OS
- Include log files if relevant

## Questions?

Open an issue for discussion!
