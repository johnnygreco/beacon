<p align="center">
  <img src="assets/beacon.png" alt="Beacon" width="800" />
</p>

Keep a signal on your long-running AI coding agents. Beacon gives you a live dashboard to see what your agents are doing, search through their conversations, and review session history — so you're never in the dark about what's happening in the background.

## Features

- **Live dashboard** — see active sessions, token usage, and a real-time activity feed
- **Session replay** — review full conversations with turn timelines and tool call details
- **Precomputed search** — find anything across all agent conversations without rebuilding an FTS index
- **Multi-agent support** — monitors Claude Code and OpenAI Codex sessions
- **Token tracking** — input, output, and cache token counts per session
- **MCP server** — give your agents access to search and review their own history

## What it looks like

<table>
  <tr>
    <td><img src="assets/beacon-screenshot.png" alt="Beacon dashboard" /></td>
    <td><img src="assets/session-screenshot.png" alt="Session view" /></td>
  </tr>
</table>

- Token usage charts and active sessions at a glance
- Live activity timeline streams tool calls and agent output as they happen
- Subagent sessions are tracked alongside their parent
- Click into any session to browse the full chat history, tool calls, and outputs

## Install

```bash
curl -sSfL https://johnnygreco.dev/beacon/install.sh | sh
```

By default the Beacon binary is placed in `~/.local/bin`, and the installer also provisions a managed ClickHouse binary under `~/.beacon/bin` when ClickHouse is not already available. Set `INSTALL_DIR` to change where `beacon` is installed:

```bash
curl -sSfL https://johnnygreco.dev/beacon/install.sh | INSTALL_DIR=/usr/local/bin sh
```

Set `INSTALL_CLICKHOUSE=0` if you already manage ClickHouse yourself and only want the Beacon binary.

To uninstall:

```bash
curl -sSfL https://johnnygreco.dev/beacon/install.sh | UNINSTALL=1 sh
```

## Quick start

```bash
beacon up
```

The dashboard opens at [http://localhost:4600](http://localhost:4600). Beacon captures Claude Code and Codex sessions automatically from the configured sources.
On first run, `beacon up` starts local ClickHouse automatically when the configured database address is local. `beacon db up` and `beacon db down` are available when you want to manage the database lifecycle explicitly.

### Build from source

Requires Go 1.24+. For local runs, you also need a ClickHouse runtime: a local `clickhouse` binary, Docker, or an already-running ClickHouse server.

```bash
git clone https://github.com/johnnygreco/beacon.git
cd beacon
make build
./bin/beacon up
```

If you already run ClickHouse yourself, point `[database]` at it and run `beacon db migrate`.

To run the live ClickHouse smoke and perf tests:

```bash
BEACON_TEST_CLICKHOUSE=127.0.0.1:9000 go test ./internal/store ./internal/search ./internal/web
BEACON_TEST_CLICKHOUSE=127.0.0.1:9000 go test ./internal/perf -bench . -run '^$'
```

## Commands

| Command | Description |
|---------|-------------|
| `beacon up` | Start the dashboard and capture services |
| `beacon down` | Stop the running Beacon server |
| `beacon run capture` | Capture without the dashboard |
| `beacon run web` | Run web and capture services |
| `beacon run mcp` | Start the MCP server (stdin/stdout) |
| `beacon status` | Show ClickHouse and index stats |
| `beacon db up` | Start local ClickHouse and migrate tables |
| `beacon db down` | Stop the local ClickHouse managed by Beacon |
| `beacon db migrate` | Create or update ClickHouse tables |
| `beacon db reset --force` | Reset the ClickHouse schema |

## Configuration

Beacon works out of the box with sensible defaults. To customize, create `~/.beacon/beacon.toml` or pass `--config <path>`.

See [`beacon.toml`](beacon.toml) for all available options.

## MCP integration

Add Beacon to your agent's MCP config to give it access to its own conversation history:

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

Available tools: **search_sessions**, **open** (retrieve context around an event), and **list_sessions**.

## Acknowledgements

This project was inspired by the awesome work of Wes McKinney ([agentsview](https://github.com/wesm/agentsview) — browsing and analyzing agent coding sessions), Eric Tramel ([moraine](https://github.com/eric-tramel/moraine) — real-time indexing of agent traces into a searchable database), and Simon Willison ([claude-code-transcripts](https://github.com/simonw/claude-code-transcripts) — turning raw session files into readable transcripts).

## License

[Apache License 2.0](LICENSE)
