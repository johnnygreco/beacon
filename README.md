# Beacon

<p align="center">
  <img src="assets/beacon.png" width="800" alt="Beacon">
</p>

Beacon collects AI agent activity from the machines you use, rolls it up in one
local dashboard, and exposes the same data to tools through MCP.

Use it for a single laptop, a team workstation, or a small fleet of machines.
The default setup keeps data local unless you deliberately point collectors at a
shared Beacon dashboard.

## Install

```bash
curl -sSfL https://johnnygreco.dev/beacon/install.sh | sh
```

The installer puts `beacon` in `$HOME/.local/bin`. Add it to your shell path if
needed:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

## Run On One Machine

Start Beacon:

```bash
beacon up
```

Open the dashboard:

```text
http://localhost:4600
```

Beacon starts the local collector and dashboard together. It can ingest Claude
Code, OpenAI Codex, Hermes Agent, OpenCode, and Pi coding-agent activity from
their usual local locations.

## Run On Multiple Machines

Use one machine as the dashboard host, then join each collector machine to it.

1. On the dashboard host, install Beacon and configure the public URL:

   ```bash
   beacon setup dashboard --collector-url https://beacon.example.com
   ```

2. Point `https://beacon.example.com` at the dashboard host. Use your reverse
   proxy, tunnel, VPN, or load balancer of choice.

3. Start the dashboard. Beacon will verify that the public URL reaches it:

   ```bash
   beacon up
   ```

4. Create an invite on the dashboard host:

   ```bash
   beacon invite
   ```

5. On each collector machine, install Beacon and run the recommended command
   printed by the invite. To join interactively and start collection in the
   foreground, use:

   ```bash
   beacon join https://beacon.example.com --start
   ```

6. On later restarts, run the collector directly:

   ```bash
   beacon collect
   ```

Check the setup from any machine:

```bash
beacon doctor setup
```

For HTTPS, reverse proxy, service manager, recovery, and invite details, see the
[production guide](docs/production.md).

## What You Get

<p align="center">
  <img src="assets/beacon-screenshot.png" alt="Beacon dashboard showing usage charts, model breakdowns, and recent sessions">
</p>

- A dashboard at `http://localhost:4600`
- Token, cost, model, project, runtime, and session summaries
- Source-aware ingestion across local AI coding tools
- MCP tools for querying sessions and usage from other agents
- Managed local ClickHouse storage, with remote ClickHouse available for larger
  installations
- Privacy controls for redaction, hashing, capture filters, and offline operation

## Common Commands

| Task | Command |
| --- | --- |
| Start dashboard and local collector | `beacon up` |
| Run only the collector | `beacon collect` |
| Open one-time setup flow | `beacon setup dashboard` |
| Join a remote dashboard | `beacon join https://beacon.example.com` |
| Create a collector invite | `beacon invite` |
| Check local and fleet setup | `beacon doctor setup` |
| Show usage summary | `beacon usage --since now-24h` |
| Show server and database health | `beacon status` |
| Query MCP tools over stdio | `beacon mcp` |
| Show all commands | `beacon --help` |

## Configuration

Beacon writes config to `~/.beacon/beacon.toml` by default. The guided setup
flow creates the files you need:

```bash
beacon setup dashboard
```

Use the docs below when you need a custom deployment, storage backend, privacy
policy, or integration surface.

## Documentation

- [Production guide](docs/production.md): multi-machine deployments, HTTPS,
  services, invites, recovery, and runbooks.
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
