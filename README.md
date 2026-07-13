# bgx - BackGround eXecute

A lightweight tool for running commands in the background, designed for parallel execution in CI/CD pipelines like GitHub Actions.

```bash
# Pull images and start Postgres + Redis in the background...
bgx fork --task-name services -- docker compose up -d --wait postgres redis

# ...while you install dependencies in the foreground (they don't need the services yet)
npm ci

# Block until the services are up, then run the tests that need them
bgx join --task-name services
npm test
```

![](https://im.dt.in.th/ipfs/bafybeigwclhzdbne6okiyzs5p7kgef4pan7ysllhlksye6ij57ss6gbroa/image.webp)

`docker compose up -d --wait` and `npm ci` run at the same time instead of one
after the other; `join` waits for the services to be ready (and fails the step
if they didn't come up) before the tests start.

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
mise use -g github:dtinth/bgx@0.3.0
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

The intended pattern: `fork` slow work that a *later* step needs but the *next*
step doesn't, keep doing that independent work meanwhile, then `join` right
before you consume the result. The join both surfaces the output and gates on
success, so a failed background task fails the step.

Good things to fork are slow and independent — their result isn't needed by the
step that runs next:

- `docker pull` / `docker compose pull` — prefetch container images
- `sudo apt-get install -y …` — install system packages
- `npm ci` (or `pip install`, `bundle install`, …) — install dependencies.
  These can be forked too, but **`join` them before any step that uses a
  dependency** (build, test, lint) — otherwise that step may run before the
  install finishes.

For example, pulling container images and installing a system tool are
independent of installing JS deps and building the app, so they overlap; the
integration tests join them at the end, right before they're needed:

```yaml
- name: Prefetch slow, independent work in the background
  run: |
    bgx fork --task-name images -- docker compose pull
    bgx fork --task-name ffmpeg -- sudo apt-get install -y ffmpeg

- name: Install deps and build (needs neither the images nor ffmpeg)
  run: |
    npm ci
    npm run build

- name: Integration tests — wait for the prefetch, fail if any failed
  run: |
    bgx join --task-name images --task-name ffmpeg
    npm run test:integration
```

No configuration is needed. On GitHub Actions, `fork` and `join` default to
`$RUNNER_TEMP/bgx.db` — the same directory for every step in a job, so they find
each other automatically. Because `RUNNER_TEMP` is unique per job (and wiped
when the job ends), concurrent jobs never collide, even on **self-hosted or
reused runners**.

Set `BGX_DB` only if you want a specific path (for example, to keep the database
outside the temp directory so it survives for inspection).

## Configuration

### Environment Variables

- **BGX_DB**: Path to the shared SQLite database. When unset, bgx uses `$RUNNER_TEMP/bgx.db` if `RUNNER_TEMP` is set (GitHub Actions), otherwise `<tmpdir>/bgx.db` (e.g. `/tmp/bgx.db`).

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

Releases are automated from git — you never push a tag by hand.
[release-please](https://github.com/googleapis/release-please) watches `main`
and, from the [Conventional Commits](https://www.conventionalcommits.org) since
the last release, maintains a **release PR** that bumps the version and updates
`CHANGELOG.md`. Merging that PR publishes the release: release-please creates the
GitHub Release + tag, then [GoReleaser](https://goreleaser.com) attaches the
cross-compiled binaries (linux/darwin/windows × amd64/arm64) as
`bgx_<Os>_<Arch>.tar.gz` archives (`.zip` on Windows) plus `checksums.txt`. The
archive names are chosen so mise's `github:` backend can resolve the right asset
automatically.

So, to cut a release:

1. Land changes on `main` using Conventional Commit messages (`feat:`, `fix:`,
   `feat!:`/`BREAKING CHANGE:` for a major bump). `feat:` drives a minor bump,
   `fix:` a patch bump.
2. Merge the release PR that release-please opens. That's it.

This is wired up in `.github/workflows/release.yml` (release-please → GoReleaser
in one run) with `release-please-config.json` and `.release-please-manifest.json`.

### One-time setup

The release workflow authenticates as a GitHub App via
[Octo STS](https://github.com/octo-sts/app) and OIDC (short-lived token, no
stored PAT), so the release PR runs CI and is authored by the app. This requires
the [Octo STS app](https://github.com/apps/octo-sts) to be installed on the
repository; the token it may request is constrained by the trust policy in
`.github/chainguard/release-please.sts.yaml`.

## Limitations

- Resource stats (CPU/memory heartbeats) only work on Linux (they read `/proc`); on macOS and Windows heartbeats are still emitted but carry zero stats.
- The shared database must live on a local filesystem — SQLite locking is unsafe over NFS, so parallel steps must share a machine, not just a database path.
- No built-in cleanup of old tasks (delete the database file, or rows, to reset).
