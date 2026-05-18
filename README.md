<p align="center">
  <img src="assets/beacon.png" alt="Beacon" width="800" />
</p>

Beacon is a local dashboard for long-running AI coding agents. It watches agent session files on your machine, normalizes their events into ClickHouse, and gives you a live dashboard, searchable history, transcript replay, and an MCP server your agents can use to inspect previous work.

## Features

- **Live dashboard** - active sessions, token usage, tool calls, errors, and recent activity over SSE
- **Session replay** - full conversations with turn timelines, tool call details, outputs, and subagent context
- **Precomputed search** - fast search across normalized session events without rebuilding an external FTS index
- **Multi-agent capture** - built-in parsers for Claude Code, OpenAI Codex, Hermes Agent, OpenCode, and Pi coding-agent sessions
- **Token and cost tracking** - input, output, cache tokens, model metadata, and configurable default pricing
- **Managed ClickHouse** - install-managed native ClickHouse, Docker fallback, or a user-managed ClickHouse server
- **MCP server** - `search_sessions`, `open`, and `list_sessions` tools for agents that need access to Beacon history

## What It Looks Like

<table>
  <tr>
    <td><img src="assets/beacon-screenshot.png" alt="Beacon dashboard" /></td>
    <td><img src="assets/session-screenshot.png" alt="Session view" /></td>
  </tr>
</table>

## Install

The installer supports macOS and Linux on `amd64` and `arm64`.

```bash
curl -sSfL https://johnnygreco.dev/beacon/install.sh | sh
```

By default, it installs:

- `beacon` to `~/.local/bin`
- a managed `clickhouse` binary to `~/.beacon/bin` when ClickHouse is not already on `PATH`

Common installer options:

```bash
# Install beacon somewhere else.
curl -sSfL https://johnnygreco.dev/beacon/install.sh | INSTALL_DIR=/usr/local/bin sh

# Install a specific Beacon release.
curl -sSfL https://johnnygreco.dev/beacon/install.sh | VERSION=0.1.0 sh

# Include prereleases when selecting the latest version.
curl -sSfL https://johnnygreco.dev/beacon/install.sh | INCLUDE_PRERELEASE=1 sh

# Skip the managed ClickHouse install if you run ClickHouse yourself.
curl -sSfL https://johnnygreco.dev/beacon/install.sh | INSTALL_CLICKHOUSE=0 sh

# Remove beacon and ~/.beacon.
curl -sSfL https://johnnygreco.dev/beacon/install.sh | UNINSTALL=1 sh
```

If `~/.local/bin` is not already on your `PATH`, add it before running `beacon`:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

## Quick Start

Start Beacon:

```bash
beacon up
```

Open [http://localhost:4600](http://localhost:4600). On first run, Beacon will:

- load `~/.beacon/beacon.toml` if it exists, otherwise use built-in defaults
- start local ClickHouse automatically when every configured ClickHouse address is local
- migrate the ClickHouse schema
- backfill existing sessions from the configured capture sources
- watch those sources for new events

Stop the web server:

```bash
beacon down
```

Check server, database, and index health:

```bash
beacon status
```

## How Capture Works

Beacon watches local session stores and writes normalized events to ClickHouse. These are the default sources:

| Source | Runtime | Default path |
|--------|---------|--------------|
| Claude Code | `claude-code` | `~/.claude/projects/**/*.jsonl` |
| OpenAI Codex | `codex` | `~/.codex/sessions/**/*.jsonl` |
| Hermes Agent | `hermes-agent` | `~/.hermes/state.db` |
| OpenCode | `opencode` | `~/.local/share/opencode/opencode*.db` |
| Pi coding agent | `pi-coding-agent` | `~/.pi/agent/sessions/**/*.jsonl` |

Backfill runs on startup by default, then Beacon keeps watching for changes. Capture can be disabled or customized in configuration.

## Configuration

Beacon reads `~/.beacon/beacon.toml` by default. Use `--config <path>` with any command to load a different file.

```bash
beacon --config ./beacon.toml up
```

Start from the example in [beacon.toml](beacon.toml). The most commonly changed settings are:

```toml
[server]
host = "0.0.0.0"
port = 4600

[database]
addrs = ["127.0.0.1:9000"]
database = "beacon"
username = "default"
password = ""
secure = false

[capture]
enabled = true
backfill_on_start = true
reconcile_interval = "30s"

[[capture.sources]]
name = "codex"
runtime = "codex"
provider = "openai"
glob = "~/.codex/sessions/**/*.jsonl"
watch_root = "~/.codex/sessions"
format = "jsonl"
```

If you point `[database].addrs` at a remote ClickHouse host, Beacon will not auto-start ClickHouse. Start that server yourself and run:

```bash
beacon db migrate
```

## Database Management

`beacon up`, `beacon serve`, and `beacon watch` try to start ClickHouse automatically only for local addresses such as `127.0.0.1:9000` or `localhost:9000`.

Auto-start chooses a runtime in this order:

1. a `clickhouse` binary on `PATH`, `BEACON_CLICKHOUSE_BIN`, or `~/.beacon/bin/clickhouse`
2. an existing Docker container named `beacon-clickhouse`
3. a new Docker container using `clickhouse/clickhouse-server:24.12`

Manual database commands:

```bash
beacon db up                         # auto-select native or Docker and migrate
beacon db up --runtime native        # require a local clickhouse binary
beacon db up --runtime docker        # require Docker
beacon db up --no-migrate            # start only
beacon db down                       # stop Beacon-managed native or Docker ClickHouse
beacon db migrate                    # create or update schema
beacon db reset --force              # destructive: drop and recreate Beacon tables
```

Native ClickHouse data is stored under `~/.beacon/clickhouse`. Docker mode uses the `beacon-clickhouse-data` volume.

## Commands

| Command | Description |
|---------|-------------|
| `beacon` | Start the web dashboard and capture service, equivalent to `beacon serve` |
| `beacon up` | Start the dashboard and capture services |
| `beacon serve` | Start the web dashboard and capture service |
| `beacon down` | Stop the running Beacon server |
| `beacon stop` | Alias for stopping the running Beacon server |
| `beacon watch` | Run capture only, without the web dashboard |
| `beacon run capture` | Run capture only |
| `beacon run web` | Run web and capture services |
| `beacon mcp` | Start the MCP server over stdin/stdout JSON-RPC |
| `beacon run mcp` | Start the MCP server over stdin/stdout JSON-RPC |
| `beacon status` | Show web server, ClickHouse, session, and search index status |
| `beacon db up` | Start local ClickHouse and migrate tables |
| `beacon db down` | Stop Beacon-managed local ClickHouse |
| `beacon db migrate` | Create or update ClickHouse tables |
| `beacon db reset --force` | Reset the ClickHouse schema and delete Beacon data |

## MCP Integration

Add Beacon to your agent's MCP config:

```json
{
  "mcpServers": {
    "beacon": {
      "command": "beacon",
      "args": ["mcp"]
    }
  }
}
```

If your MCP client cannot use Beacon's config file, pass ClickHouse directly:

```json
{
  "mcpServers": {
    "beacon": {
      "command": "beacon",
      "args": ["mcp", "--clickhouse", "127.0.0.1:9000"]
    }
  }
}
```

Available tools:

- `search_sessions` - search the precomputed activity index
- `open` - retrieve an event with surrounding context from the same session
- `list_sessions` - list recent sessions and summary statistics

Run `beacon up` or `beacon db up` before using MCP so ClickHouse is available and migrated.

## Build From Source

Source builds require Go 1.24.1 or newer. Node/npm are only needed for Playwright tests and vendoring browser assets.

```bash
git clone https://github.com/johnnygreco/beacon.git
cd beacon
make build
./bin/beacon up
```

Useful development commands:

```bash
make generate       # regenerate templ output
make test           # generate templates and run Go tests
make test-race      # run Go tests with the race detector
make perf-bench     # run perf benchmarks; set PERF_SIZE=small|medium|large
npm install         # install Playwright and asset vendoring dependencies
npm run vendor      # refresh vendored frontend assets
npm run test:e2e    # dashboard and search Playwright tests
npm run test:a11y   # accessibility tests
npm run test:visual # visual regression tests
```

Most Go tests do not need a live ClickHouse server. Live ClickHouse integration and perf tests are opt-in:

```bash
beacon db up
BEACON_TEST_CLICKHOUSE=127.0.0.1:9000 go test ./internal/store ./internal/search ./internal/web
BEACON_TEST_CLICKHOUSE=127.0.0.1:9000 go test ./internal/perf -bench . -run '^$'
```

Playwright tests start their own e2e server by default. To test against an already running Beacon-compatible server, set `BEACON_E2E_BASE_URL`.

## Troubleshooting

**`beacon` is not found after install.** Add the install directory to your `PATH`, usually `export PATH="$HOME/.local/bin:$PATH"`.

**ClickHouse does not start.** Run `beacon db up --runtime native` if you installed the managed ClickHouse binary, or `beacon db up --runtime docker` if you prefer Docker. Set `BEACON_CLICKHOUSE_BIN=/path/to/clickhouse` to use a specific binary.

**Beacon connects to the wrong database.** Check `[database].addrs` in `~/.beacon/beacon.toml` or pass `--config <path>`.

**No sessions appear.** Confirm the agent has written session files in one of the configured source paths, then run `beacon status` and restart with `backfill_on_start = true`.

**Port 4600 is already in use.** Change `[server].port` in the config file or stop the existing process with `beacon down`.

## Acknowledgements

This project was inspired by Wes McKinney's [agentsview](https://github.com/wesm/agentsview), Eric Tramel's [moraine](https://github.com/eric-tramel/moraine), and Simon Willison's [claude-code-transcripts](https://github.com/simonw/claude-code-transcripts).

## License

[Apache License 2.0](LICENSE)
