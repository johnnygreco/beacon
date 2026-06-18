# Production Guide

This guide covers running Beacon as a single-machine service. Beacon captures
local AI-agent activity, stores it in ClickHouse, serves the dashboard on the
same host, and exposes MCP over local stdio.

If you expose Beacon outside loopback, put it behind infrastructure you control
and authenticate that external access before traffic reaches Beacon.

## Install

Install the current Beacon release:

```bash
curl -sSfL https://johnnygreco.dev/beacon/install.sh | sh
export PATH="$HOME/.local/bin:$PATH"
beacon --version
```

The installer places `beacon` in `~/.local/bin` by default. It also installs a
managed ClickHouse binary under `~/.beacon/bin` when ClickHouse is not already on
`PATH`.

For a source checkout during manual testing:

```bash
make install-local INSTALL_DIR="$HOME/.local/bin"
beacon --version
```

## Local Service

Start Beacon:

```bash
beacon up
```

With default settings, Beacon binds the web dashboard to `127.0.0.1:4600` and
starts managed local ClickHouse when needed. Open:

```text
http://localhost:4600
```

Check local health:

```bash
beacon status
beacon doctor setup
curl -fsS http://localhost:4600/health
```

## Configuration

Beacon can run without a config file. For persistent settings, create
`~/.beacon/beacon.toml`:

```toml
[server]
host = "127.0.0.1"
port = 4600

[database]
addrs = ["127.0.0.1:9000"]
database = "beacon"
username = "default"
password = ""
secure = false

[dashboard]
name = "Beacon"
```

Keep the server bound to loopback for ordinary use. Beacon rejects non-loopback
dashboard hosts unless you deliberately configure a trusted boundary outside
Beacon.

## Reverse Proxy Or VPN Access

For browser access from another machine, prefer a private tunnel, SSH port
forward, Tailscale, or a TLS reverse proxy that authenticates users before
forwarding traffic to Beacon.

Keep Beacon itself bound to loopback:

```toml
[server]
host = "127.0.0.1"
port = 4600
```

Configure the proxy upstream to send `Host: 127.0.0.1:4600` or
`Host: localhost:4600`. Beacon's loopback host guard rejects unrelated Host
headers so DNS rebinding or accidental public exposure cannot silently reach the
dashboard.

If you need an external hostname, terminate TLS and authentication in the proxy
or VPN layer. Beacon's dashboard, JSON API, and `/api/mcp` trust the local
network boundary once the request reaches the process.

## ClickHouse

The simplest setup keeps ClickHouse on the same machine as Beacon:

```bash
beacon db up
beacon up
```

When `[database].addrs` points at `127.0.0.1:9000`, `beacon up` can start
Beacon-managed local ClickHouse for you. Native ClickHouse data lives under
`~/.beacon/clickhouse`; Docker mode uses the `beacon-clickhouse-data` Docker
volume.

If ClickHouse is remote, start and secure it yourself, then migrate from a
trusted machine:

```toml
[database]
addrs = ["clickhouse.example.com:9440"]
database = "beacon"
username = "beacon"
password = "..."
secure = true
```

```bash
beacon db migrate
```

Use ClickHouse native TCP over TLS on port `9440` for remote database access.
Use plaintext port `9000` only on loopback, a private network you trust, or an
SSH tunnel.

## MCP

Run Beacon on the same machine as the MCP client:

```bash
beacon up
```

Configure the client to launch Beacon over stdio:

```bash
beacon mcp
```

The MCP server does not run capture, but it opens the writable Beacon store so
annotation tools can persist notes. Startup may run schema migrations on the
configured database, matching `beacon up`. For details, see
[MCP Integration](mcp.md).

## Service Managers

For personal production, run Beacon under your user account or a dedicated
service account. Keep config, ClickHouse data, and logs owned by that account.

Linux systemd unit:

```ini
[Unit]
Description=Beacon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%h/.local/bin/beacon up
Restart=on-failure
RestartSec=5
Environment=PATH=%h/.beacon/bin:%h/.local/bin:/usr/local/bin:/usr/bin:/bin

[Install]
WantedBy=default.target
```

macOS LaunchAgent sketch:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>dev.beacon</string>
  <key>ProgramArguments</key>
  <array>
    <string>/Users/YOU/.local/bin/beacon</string>
    <string>up</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>/Users/YOU/Library/Logs/beacon.log</string>
  <key>StandardErrorPath</key>
  <string>/Users/YOU/Library/Logs/beacon.err</string>
</dict>
</plist>
```

Replace `YOU` with the local account.

## Operations Checklist

- Install the intended Beacon release or local build.
- Keep Beacon bound to `127.0.0.1` unless protected by a trusted proxy or VPN.
- Start Beacon with `beacon up`.
- Run `beacon status` and `beacon doctor setup`.
- Verify the dashboard at `http://localhost:4600`.
- Verify MCP clients launch `beacon mcp` over stdio.
- Confirm captured sessions advance after running a supported local AI coding
  tool.

## Recovery

Stop Beacon:

```bash
beacon down
```

Start it again:

```bash
beacon up
```

If ClickHouse was interrupted, check status first:

```bash
beacon status
```

Then rerun migrations from the same machine:

```bash
beacon db migrate
```

Beacon does not delete original agent session files during normal recovery. If
you need to reset derived Beacon data, stop Beacon first and back up
`~/.beacon` before removing ClickHouse data or generated indexes.

## Troubleshooting

If the dashboard is unreachable:

1. Confirm the process is running with `beacon status`.
2. Check that the configured port is listening on loopback.
3. Curl the local health endpoint:

   ```bash
   curl -fsS http://localhost:4600/health
   ```

4. If using a proxy or VPN, confirm the upstream forwards to `127.0.0.1:4600`
   with a loopback Host header.
5. Check Beacon logs from your terminal, systemd unit, LaunchAgent, or tmux
   session.

If MCP tools return database errors, start Beacon locally and verify ClickHouse:

```bash
beacon up
beacon status
```
