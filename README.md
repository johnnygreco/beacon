# Beacon

<p align="center">
  <img src="assets/beacon.png" width="800" alt="Beacon">
</p>

Beacon collects AI agent activity on the machine you use, rolls it up in a
dashboard on that same machine, and exposes the same data to tools through MCP.

Beacon is currently a single-machine tool: capture, storage, dashboard, API, and
MCP all run locally.

## Install

```bash
curl -sSfL https://johnnygreco.dev/beacon/install.sh | sh
```

The installer puts `beacon` in `$HOME/.local/bin`. Add it to your shell path if
needed:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

You can also install only the Beacon CLI with Go:

```bash
go install github.com/johnnygreco/beacon@latest
```

The Go install path does not install the managed ClickHouse binary. Use the
shell installer above, install `clickhouse` on `PATH`, set
`BEACON_CLICKHOUSE_BIN`, or run Beacon against Docker/another ClickHouse
endpoint when local native ClickHouse is needed.

## Run On One Machine

Start Beacon:

```bash
beacon up
```

Open the dashboard:

```text
http://localhost:4600
```

Beacon starts local capture, storage, and the dashboard together. It can ingest
Claude Code, OpenAI Codex, Hermes Agent, OpenCode, and Pi coding-agent activity
from their usual local locations.

## What You Get

<p align="center">
  <img src="assets/beacon-screenshot.png" alt="Beacon dashboard showing usage charts, model breakdowns, and recent sessions">
</p>

- A dashboard at `http://localhost:4600`
- Token, cost, model, project, runtime, and session summaries
- Source-aware capture across local AI coding tools
- MCP tools for querying sessions and usage from other agents
- Managed local ClickHouse storage, with optional direct ClickHouse
  configuration for trusted local or private-network deployments
- Privacy controls for redaction, hashing, capture filters, and offline operation

Check the local setup:

```bash
beacon doctor setup
```

For local service, reverse proxy, and recovery guidance, see the
[production guide](docs/production.md).

## Common Commands

| Task | Command |
| --- | --- |
| Start local capture, storage, and dashboard | `beacon up` |
| Check local setup | `beacon doctor setup` |
| Show usage summary | `beacon usage --since now-24h` |
| Show server and database health | `beacon status` |
| Query MCP tools over stdio | `beacon mcp` |
| Show all commands | `beacon --help` |

## Configuration

The default local run does not need a config file; `beacon up` can start the
dashboard with built-in defaults. When you do want persistent settings, Beacon
reads `~/.beacon/beacon.toml`.

Use the docs below when you need a custom local setting, storage backend,
privacy policy, or integration surface.

## Documentation

- [Production guide](docs/production.md): local service setup, reverse proxy,
  recovery, and runbooks.
- [Privacy model](docs/privacy.md): captured data, redaction, hashing, and
  operator responsibilities.
- [MCP integration](docs/mcp.md): tools, filters, stdio usage, and Claude
  Desktop setup.
- [Usage analytics](docs/usage.md): token and cost accounting with local JSONL
  or ClickHouse.
- [ClickHouse storage](docs/clickhouse.md): schema, setup, migrations, and
  retention.
- [Development guide](docs/development.md): source builds, tests, generated
  files, and visual checks.
- [Architecture](docs/architecture.md): ingestion pipeline, storage model, and
  UI shape.
- [Toolchain](docs/toolchain.md): pinned tool versions and dependency update
  workflow.
- [Release process](docs/release.md): release builds, checksums, and publishing.

## Build From Source

```bash
git clone https://github.com/johnnygreco/beacon.git
cd beacon
make build
./bin/beacon up
```

For day-to-day development commands, see the
[development guide](docs/development.md).

## Acknowledgements

Beacon was inspired by Wes McKinney's
[agentsview](https://github.com/wesm/agentsview), Eric Tramel's
[moraine](https://github.com/eric-tramel/moraine), and Simon Willison's
[claude-code-transcripts](https://github.com/simonw/claude-code-transcripts).

The Beacon logo includes the lighthouse glyph by Freepik on
[Flaticon](https://www.flaticon.com/free-icons/lighthouse), used under the
Flaticon license.

## License

[Apache License 2.0](LICENSE)
