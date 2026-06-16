# Development Guide

This guide covers day-to-day Beacon development from a source checkout. For
production deployment, see [production.md](production.md). For dependency and
toolchain update policy, see [toolchain.md](toolchain.md).

## Prerequisites

- Go 1.24.1 or newer
- Node.js and npm for browser asset vendoring and Playwright tests
- make
- Optional: ClickHouse for integration and performance tests

See [toolchain.md](toolchain.md) before changing pinned CI or release tooling.

## Build From Source

```bash
git clone https://github.com/johnnygreco/beacon.git
cd beacon
make build
./bin/beacon up
```

Install the workspace binary locally:

```bash
make install-local INSTALL_DIR="$HOME/.local/bin"
```

## Common Development Commands

| Task | Command |
| --- | --- |
| Build backend and frontend | `make build` |
| Install local binary | `make install-local INSTALL_DIR="$HOME/.local/bin"` |
| Run all Go tests | `make test` |
| Run Go tests with race and coverage gates | `make test-cover` |
| Run JavaScript lint and unit tests | `npm run test:frontend` |
| Run fast backend benchmarks | `make perf-fast` |
| Run performance benchmarks | `make perf-bench` |
| Run browser performance tests | `make perf-browser` |
| Run local performance lab smoke test | `make perf-lab-smoke` |
| Run Playwright dashboard tests | `npm run test:e2e` |
| Run accessibility tests | `npm run test:a11y` |
| Run visual regression tests | `npm run test:visual` |
| Publish a release | `make publish VERSION=x.y.z` |
| Remove generated artifacts | `make clean` |

Most test commands keep Beacon user data intact. `make clean` removes build and
test artifacts such as `bin/`, `dist/`, coverage files, Playwright reports, and
test results.

## Generated Files

Beacon embeds dashboard assets and configuration templates in the Go binary.
Some generated files are intentionally kept out of source control:

- `internal/web/static/`
- `internal/config/templates/*.generated.go`

Regenerate them with:

```bash
make generate
```

`make build` and `make test` run generation automatically. Templ output is
different: commit both the `.templ` source and matching `_templ.go` file, and
use `make generate-check` before opening a PR to catch stale generated templates.

## Coverage Gates

Run the Go coverage gate:

```bash
make test-cover
```

CI checks package thresholds from `coverage.thresholds` with
`scripts/check-coverage.sh`. Update that file in the same change as any
intentional threshold adjustment.

The generated profile can be inspected with:

```bash
go tool cover -html=coverage.txt
```

Frontend unit tests:

```bash
npm run test:frontend
```

## ClickHouse Tests

Most Go tests do not need ClickHouse. Live ClickHouse integration and benchmark
paths are opt-in:

```bash
beacon db up
BEACON_TEST_CLICKHOUSE=127.0.0.1:9000 go test ./internal/store ./internal/search ./internal/web
BEACON_TEST_CLICKHOUSE=127.0.0.1:9000 go test ./internal/perf -bench . -run '^$'
```

`make perf-fast` runs deterministic backend benchmarks without ClickHouse.
`make perf-lab-smoke` installs the workspace binary, seeds a disposable
ClickHouse database, serves Beacon locally, and writes reports under
`test-results/perf/lab/latest/`.

```bash
make perf-fast
make perf-lab-smoke
```

## Visual Checks

The dashboard has Playwright coverage for core views and screenshots.

```bash
npm run test:e2e
npm run test:visual
npm run test:visual -- --update-snapshots
```

Use visual updates only when intentional UI changes require new baselines.

## Release Work

Release builds, checksums, install-script validation, and publishing steps live
in [release.md](release.md). The publish entrypoint is:

```bash
make publish VERSION=x.y.z
```
