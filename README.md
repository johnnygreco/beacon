<p align="center">
  <img src="assets/beacon.png" alt="Beacon" width="800" />
</p>

Keep a signal on your long-running AI coding agents. Beacon gives you a live dashboard to see what your agents are doing, search through their conversations, and review session history — so you're never in the dark about what's happening in the background.

## Features

- **Live dashboard** — see active sessions, token usage, and a real-time activity feed
- **Session replay** — review full conversations with turn timelines and tool call details
- **Full-text search** — find anything across all your agent conversations
- **Multi-agent support** — monitors Claude Code and OpenAI Codex sessions
- **Token & cost tracking** — input, output, and cache token counts per session
- **MCP server** — give your agents access to search and review their own history

## Install

```bash
curl -sSfL https://johnnygreco.dev/beacon/install.sh | sh
```

By default the binary is placed in `~/.local/bin`. Set `INSTALL_DIR` to change it:

```bash
curl -sSfL https://johnnygreco.dev/beacon/install.sh | INSTALL_DIR=/usr/local/bin sh
```

To uninstall:

```bash
curl -sSfL https://johnnygreco.dev/beacon/install.sh | UNINSTALL=1 sh
```

## Quick start

```bash
beacon serve
```

The dashboard opens at [http://localhost:4600](http://localhost:4600). Beacon picks up Claude Code and Codex sessions automatically — no configuration needed.

### Build from source

Requires Go 1.24+.

```bash
git clone https://github.com/johnnygreco/beacon.git
cd beacon
make build
./bin/beacon serve
```

## Commands

| Command | Description |
|---------|-------------|
| `beacon serve` | Start the dashboard and begin monitoring |
| `beacon watch` | Monitor without the dashboard (headless) |
| `beacon mcp` | Start the MCP server (stdin/stdout) |
| `beacon status` | Show database and index stats |
| `beacon db migrate` | Run schema migrations |
| `beacon db reset --force` | Reset the database |

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

Available tools: **search**, **open** (retrieve context around an event), and **list_sessions**.

## License

[Apache License 2.0](LICENSE)
