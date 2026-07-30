# Watchtower — Ubuntu Image Build Pipeline Monitor

Watchtower monitors Ubuntu image build pipelines via proactive Temporal cron workflows
and a reactive Mattermost bot. See [DESIGN.md](DESIGN.md) for architecture, data types,
and demo flows.

## Environment variables
MATTERMOST_WEBHOOK_URL        # Mattermost incoming webhook URL — optional; unset = stdout simulation
TEST_OBSERVER_URL             # Ubuntu Test Observer API base URL (default: https://tests-api.ubuntu.com)
TEMPORAL_HOST                 # Temporal server (default: localhost:7233)
WATCHTOWER_RELEASES_SCOPE     # Comma-separated ordered list of releases to scope ALL operations
                              # (fetch, diff, summary, failures, tests); empty = all releases

# TODO: re-add when log analysis is implemented
# OPENROUTER_API_KEY   # OpenRouter API key
# LLM_MODEL            # OpenRouter model slug

## Key conventions
- Port interfaces (ArtefactSource, BuildSource, Notifier, LLMClient, LogFetcher, SnapshotStore)
  live in `internal/ports/`; always have a mock/stub impl for tests in the adapter package
- Errors wrapped with context: fmt.Errorf("activityName: %w", err)
- snapshot.json written atomically (write to tmp file, rename)
- Dependency direction: cmd/bot → adapters → application → ports → domain
- Never import internal packages circularly — domain has no deps, ports depends only on domain
- application.Dispatch is pure (no I/O side effects beyond the ports.Notifier) — easy to unit test

## Running locally

### Option B — 2 terminals (recommended for active development)
```
# terminal 1
temporal server start-dev

# terminal 2
make run-bot   # or: go run ./cmd/bot/
```
Type commands at the `you>` prompt. No API keys required.

To restart after a code change: `Ctrl-C` in terminal 2, then `make run-bot` again.
To wipe the local snapshot and start fresh: `make clean-state` before `make run-bot`.

### Option A — Docker Compose (demo / staging)
```
cp .env.example .env          # no required vars; optionally set MATTERMOST_WEBHOOK_URL
make up                       # builds images, starts temporal + bot
make down                     # tear down (state volume is preserved)
```
Services exposed:
- http://localhost:8233  — Temporal Web UI

Note: the REPL runs inside the bot container. Attach with:
`docker attach $(docker compose ps -q bot)` — detach with `Ctrl-P Ctrl-Q`.
For interactive development prefer Option B.

## Makefile targets
| Target               | What it does                                                       |
|----------------------|--------------------------------------------------------------------|
| `make build`         | Compile bot binary into `bin/`                                     |
| `make clean`         | Remove `bin/`                                                      |
| `make test`          | `go test -race -count=1 ./...`                                     |
| `make lint`          | `golangci-lint run ./...`                                          |
| `make check`         | lint + test (pre-commit gate)                                      |
| `make run-bot`       | `go run ./cmd/bot/` (Option B)                                     |
| `make clean-state`   | Delete `state/snapshot.json` (force fresh fetch on next start)     |
| `make up`            | `docker compose up --build -d` (Option A)                          |
| `make down`          | `docker compose down` (keeps volumes)                              |
| `make restart-bot`   | Rebuild + restart only the bot container (Temporal keeps running)  |
| `make reset`         | `down -v` + `up` — full wipe of all volumes and fresh start        |
| `make rock`          | Build OCI rock and push to MicroK8s registry (`localhost:32000`)   |
| `make charm-pack`    | Clean + pack the charm with charmcraft                             |
| `make charm-refresh` | `rock` + `charm-pack` + `juju refresh watchtower`                  |
| `make juju-status`   | `juju status --relations`                                          |
| `make juju-logs`     | `juju debug-log --tail`                                            |

## Test strategy
Test these:   domain/artefact.go (pure helpers), state/snapshot.go (diff logic),
              activities/analyze_log.go (JSON parsing), application/commands.go (command routing),
              application/formatters.go (markdown formatting)
Skip today:   Temporal workflow sequencing, real HTTP clients

## Development rules

### Keeping docs in sync
When adding a new feature or making a significant design change, update both:
- **DESIGN.md** — project structure, data types, demo flows, tech stack
- **CLAUDE.md** — conventions, env vars, test strategy, if those change

### Before every commit
ALWAYS run the pre-commit gate and fix all failures before committing:
```
make check   # runs lint then test
```
Or individually:
```
make lint    # golangci-lint run ./...
make test    # go test -race -count=1 ./...
```
Never commit code that fails either check. Install golangci-lint once:
```
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

### CI/CD (GitHub Actions)
Two jobs run on every push and PR to `main` (`.github/workflows/ci.yml`):
- **lint**: golangci-lint via `golangci/golangci-lint-action`
- **test**: `go build ./...` then `go test -race -count=1 ./...`

Both jobs must be green before a branch is merged. Do not merge PRs with failing CI.

### Commit message conventions
- Use the imperative mood, present tense: "add dispatch handler" not "added"
- Subject line ≤ 72 characters
- Format: `<type>: <subject>` where type is one of:
  - `feat` — new feature or behaviour
  - `fix` — bug fix
  - `test` — adding or fixing tests
  - `refactor` — restructuring without behaviour change
  - `chore` — tooling, deps, CI, config
  - `docs` — documentation only
- One logical change per commit; don't bundle unrelated changes
- Reference block number when completing a build-order block: e.g. `feat(block-2): add mattermost dispatch package`
