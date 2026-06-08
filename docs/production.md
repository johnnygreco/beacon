# Personal production guide

Beacon's production target is one owner running a central dashboard for agent
activity from arbitrary enrolled machines. Those machines can be a laptop, home
server, Mac mini, Linux VM, cloud VM, or any other host that can reach the
central Beacon server over HTTPS.

This is not an enterprise deployment guide. Beacon does not provide multi-tenant
hosting, SSO, compliance controls, or arbitrary-secret DLP. Treat it as a
personal production system: secure the host, keep tokens private, expose only
the control plane you need, and assume captured data remains sensitive even
after redaction.

## Architecture

The production layout has one control plane and zero or more collectors.

```text
agent session files
  -> beacon collect on each enrolled machine
  -> local redacted spool on that collector
  -> HTTPS ingest to central Beacon
  -> ClickHouse and control-plane metadata on the central host
  -> dashboard, API, and remote MCP reads from the unified dataset
```

Control-plane host:

- runs `beacon up`
- owns the dashboard and HTTP ingest endpoints
- owns ClickHouse writes and schema migration
- stores control-plane metadata at `[fleet].metadata_path`
- creates owner and enrollment tokens with `beacon init`
- serves dashboard/API/MCP reads from all enrolled collectors by default

Collector host:

- runs `beacon collect`
- reads the configured local capture sources
- applies Beacon's best-effort destructive redaction policy before spool
- writes owner-only pending batches under `[fleet].spool_dir`
- sends gzip JSON batches to the control plane
- advances local collector state only after committed acknowledgements

MCP client host:

- runs `beacon mcp`
- usually proxies to the control plane with `--remote-url`
- does not need ClickHouse credentials in remote mode
- searches the unified central dataset by default unless scoped by token or
  explicit node, collector, source, runtime, or project filters

## Personal topologies

Use whatever topology matches the machines you actually run. These are examples,
not required personas or hardcoded source layouts.

| Topology | Control plane | Collectors | Notes |
|----------|---------------|------------|-------|
| Single workstation | same machine | same machine with `[fleet].role = "both"` | Best local development path. Loopback auth is enough while bound to `127.0.0.1`. |
| Home server dashboard | home server or Mac mini | laptop, desktop, home server, VMs | Use HTTPS reverse proxy and run each remote host as `[fleet].role = "collector"`. |
| Cloud dashboard | cloud VM | laptops, home machines, cloud VMs | Keep ClickHouse private to the VM or a private network. Expose only HTTPS Beacon routes. |
| Local dashboard plus remote readers | workstation | optional collectors | Agent MCP clients can use `beacon mcp --remote-url` from any trusted machine. |

## Install

Install the same Beacon release on the control plane, collectors, and MCP client
machines:

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

Do not mix unreviewed local builds and release builds across machines unless you
are intentionally testing that exact build.

## Control-plane config

Start from `~/.beacon/beacon.toml` or the repo's `beacon.toml` example. A
central control-plane host that does not capture local source files can use:

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
name = "Beacon Control Plane"

[auth]
mode = "loopback"
cookie_name = "beacon_owner_token"
allow_insecure_owner_http = false

[fleet]
role = "control-plane"
metadata_path = "~/.beacon/control-plane.db"
node_name = "Beacon Control Plane"
```

Run the server:

```bash
beacon up
```

For browser access from another machine, keep Beacon bound to loopback and put a
TLS reverse proxy in front of it:

```toml
[server]
host = "127.0.0.1"
port = 4600

[auth]
mode = "reverse-proxy"
```

The reverse proxy is responsible for authenticating you before traffic reaches
Beacon. Because Beacon is still bound to `127.0.0.1`, configure the proxy's
upstream request `Host` as `127.0.0.1:4600` or `localhost:4600`; Beacon's
loopback guard rejects forwarded public hosts such as `beacon.example.com`.

In this loopback reverse-proxy layout, the dashboard, JSON API, and `/api/mcp`
trust the proxy boundary. The proxy must authenticate every externally
reachable route, including `/api/mcp`. Beacon read-token enforcement and scoped
read-token filtering for `/api/mcp` are active when Beacon runs in owner-token
mode, or when reverse-proxy mode is bound to a non-loopback private interface
that only the trusted proxy can reach.

Owner-token mode is available for personal API or browser access:

```toml
[server]
host = "127.0.0.1"
port = 4600

[auth]
mode = "owner-token"
allow_insecure_owner_http = false
```

For non-loopback plain HTTP, Beacon refuses owner-token mode unless
`allow_insecure_owner_http = true`. Use that opt-in only for a private tunnel or
temporary debugging. Normal production browser access should terminate TLS at a
trusted reverse proxy and use `auth.mode = "reverse-proxy"`.

## ClickHouse

The simplest personal production setup keeps ClickHouse on the same host as the
control plane:

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
SSH tunnel. Normal remote collectors should not receive ClickHouse credentials;
they should use `beacon collect` and HTTP ingest instead.

## Initialize owner and enrollment tokens

On the control-plane host:

```bash
beacon init --enroll-ttl 30m
```

The command prints two secrets once:

- an owner token for full personal read/admin access
- a one-use enrollment token for collectors

Store the owner token in a local password manager. Do not paste enrollment or
owner tokens into shell history, issue comments, logs, or MCP prompts. Pass
enrollment tokens through stdin or an environment variable name.

If you need another enrollment token later, run `beacon init --enroll-ttl 30m`
again on the control plane. Existing owner/control-plane metadata is reused and
new owner and enrollment tokens are shown once.

## Collector config

On each collector machine, configure `[fleet].role = "collector"` and the local
capture sources for that host:

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

[fleet]
role = "collector"
metadata_path = "~/.beacon/control-plane.db"
control_plane_url = "https://beacon.example.com"
node_name = "Laptop"
ingest_token_file = "~/.beacon/ingest-token"
ingest_token_env = "BEACON_INGEST_TOKEN"
spool_dir = "~/.beacon/spool"
spool_max_bytes = 268435456
spool_batch_size = 500
retry_min = "1s"
retry_max = "1m"
heartbeat_interval = "30s"

[[capture.sources]]
name = "codex"
runtime = "codex"
provider = "openai"
glob = "~/.codex/sessions/**/*.jsonl"
watch_root = "~/.codex/sessions"
format = "jsonl"
```

Collector configs do not need working ClickHouse settings because
`beacon collect` never writes directly to ClickHouse. The database block can
remain at defaults unless you also use local debugging commands on that machine.

Supported runtime/format pairs are:

- `claude-code/jsonl`
- `codex/jsonl`
- `hermes-agent/sqlite`
- `opencode/sqlite`
- `pi-coding-agent/jsonl`

Keep source names stable. Beacon uses node, collector, source, runtime, and
project metadata to make sessions searchable across the unified dashboard and
MCP dataset.

## Enroll collectors

On the collector machine:

```bash
printf 'Enrollment token: ' >&2
read -r -s BEACON_ENROLL_TOKEN
printf '\n' >&2
printf '%s\n' "$BEACON_ENROLL_TOKEN" | beacon enroll https://beacon.example.com --token-stdin
unset BEACON_ENROLL_TOKEN
```

If your shell does not support silent `read -s`, paste the token from a password
manager directly into the stdin form instead of putting the token literal in a
shell command:

```bash
beacon enroll https://beacon.example.com --token-stdin
```

Paste the token, press Enter, then send EOF with `Ctrl-D`.

Successful enrollment writes:

- local collector metadata at `[fleet].metadata_path`
- a bound ingest token at `[fleet].ingest_token_file`
- source assignments for the configured capture sources
- the current control-plane schema epoch

The enrollment token cannot ingest data. The returned ingest token is bound to
the assigned node, collector, source list, and epoch.

To re-enroll an existing collector, keep its current ingest token file in place
and run the same `beacon enroll` command with a fresh enrollment token. Beacon
uses the existing ingest token as proof that this machine owns the current
collector identity, then rotates the ingest token file after successful
enrollment.

## Run collectors

Run a one-cycle smoke test first:

```bash
beacon collect --once
```

Then run continuously:

```bash
beacon collect
```

If the control-plane URL is not in config, pass it for that run:

```bash
beacon collect --control-plane-url https://beacon.example.com
```

Expected behavior:

- source files are scanned and normalized locally
- redaction runs before any collector spool write
- pending batches are written under `[fleet].spool_dir`
- batches are sent to `/api/ingest/v1/batches`
- local state advances only after the control plane acknowledges commit
- heartbeats report source status, queue depth, and spool bytes

When the control plane is offline, the collector keeps retrying and leaves
pending data in the spool until `[fleet].spool_max_bytes` is reached. If the
spool is full, collector reads pause before advancing unacknowledged state.

## Run as services

For personal production, run the control plane and each collector under your
host's ordinary service manager.

Linux control-plane systemd unit:

```ini
[Unit]
Description=Beacon control plane
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%h/.local/bin/beacon up
Restart=on-failure
RestartSec=5
Environment=PATH=%h/.local/bin:/usr/local/bin:/usr/bin:/bin

[Install]
WantedBy=default.target
```

Linux collector systemd unit:

```ini
[Unit]
Description=Beacon collector
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%h/.local/bin/beacon collect
Restart=on-failure
RestartSec=5
Environment=PATH=%h/.local/bin:/usr/local/bin:/usr/bin:/bin

[Install]
WantedBy=default.target
```

macOS LaunchAgent collector sketch:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>dev.beacon.collect</string>
  <key>ProgramArguments</key>
  <array>
    <string>/Users/YOU/.local/bin/beacon</string>
    <string>collect</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>/Users/YOU/Library/Logs/beacon-collect.log</string>
  <key>StandardErrorPath</key>
  <string>/Users/YOU/Library/Logs/beacon-collect.err</string>
</dict>
</plist>
```

Replace `YOU` with the local account. Keep config, spool, metadata, and token
files owned by that account.

## Remote MCP setup

Remote MCP mode is the normal production path for agents on machines that do not
own ClickHouse credentials:

```bash
BEACON_MCP_URL=https://beacon.example.com/api/mcp \
BEACON_READ_TOKEN="$BEACON_READ_TOKEN" \
beacon mcp
```

Equivalent flags:

```bash
beacon mcp \
  --remote-url https://beacon.example.com/api/mcp \
  --read-token-file ~/.beacon/read-token
```

Use an owner, admin, or read-scoped token with read scope when Beacon token auth
is active for `/api/mcp`. The owner token shown by `beacon init` works for a
full personal dataset. If you use a scoped read token in an auth-enforced MCP
layout, Beacon silently applies the token's node, collector, or source scope,
and tool results include effective scope metadata. Explicit tool filters can
further scope by node, collector, source, runtime, or project. In the loopback
reverse-proxy layout above, the proxy owns MCP authentication and Beacon does
not apply read-token scoping to `/api/mcp`.

Remote MCP URLs must use HTTPS for non-loopback hosts. Plain HTTP is accepted
only for loopback development.

See [MCP Integration](mcp.md) for Claude Code, Codex, generic MCP client JSON,
tool arguments, and direct ClickHouse debugging mode.

## Dashboard and API checks

After starting or changing production config, run:

```bash
beacon status
curl -fsS https://beacon.example.com/health
```

On the dashboard:

- active and completed sessions should include all enrolled collectors by
  default
- node, collector, source, runtime, and project filters should narrow the same
  unified dataset
- recent activity should advance after a collector sends new batches
- the browser should connect over HTTPS when accessed off-host

For owner-token API auth, use a read-capable token:

```bash
curl -fsS \
  -H "Authorization: Bearer $BEACON_READ_TOKEN" \
  https://beacon.example.com/api/dashboard/fleet
```

For MCP, use the remote MCP ping in the troubleshooting runbook below.

## Redaction and minimization

Beacon runs `redact-v1` before data reaches durable capture storage:

- local capture redacts normalized events before ClickHouse rows are built
- `beacon collect` redacts normalized events, capture errors, and checkpoints
  before writing collector spool files
- HTTP ingest redacts accepted batches before ClickHouse commit

The policy is destructive and best effort. It covers Beacon token formats,
common credential formats, configured `[redaction].path_masks`, configured
`[redaction].env_masks`, configured `[redaction].literal_masks`, and explicit
fixture values used by tests. It does not guarantee detection of every arbitrary
secret pasted into a prompt, response, path, tool argument, or tool output.

Add personal masks for values you know should never be stored:

```toml
[redaction]
path_masks = [
  "/Users/you/private-project",
  "/srv/secrets"
]
env_masks = [
  "OPENAI_API_KEY",
  "ANTHROPIC_API_KEY",
  "GITHUB_TOKEN",
  "BEACON_OWNER_TOKEN",
  "BEACON_ENROLL_TOKEN",
  "BEACON_INGEST_TOKEN",
  "BEACON_READ_TOKEN"
]
literal_masks = [
  "internal-hostname.example",
  "personal-fixture-secret"
]
```

If you set `env_masks`, include every default environment variable name you
still want masked; the configured list replaces the default list.

Changing the policy does not rewrite existing ClickHouse rows. To apply a new
policy to old captured data, reset/replay or reingest from source files that are
still available.

See [Privacy, retention, and local data boundaries](privacy.md) for storage
locations and retention behavior.

## Production checklist

Before relying on Beacon as your personal production dashboard:

- install the same intended build on the control plane and collectors
- keep the control plane behind HTTPS for non-loopback browser or MCP access
- choose `reverse-proxy` or `owner-token` auth deliberately
- run `beacon init` and store the owner token outside shell history
- enroll each collector with a short-lived one-use enrollment token
- verify each collector with `beacon collect --once`
- run each long-lived process under systemd, launchd, tmux, or another
  supervised process manager
- run `beacon status` on the control plane
- verify dashboard active/completed views across at least two scopes
- verify `beacon mcp --remote-url ...` from at least one remote agent machine
- review `[redaction]` masks for private paths, environment variables, and known
  literal values
- confirm backups or intentional non-backup policy for `~/.beacon` and
  ClickHouse data
- keep release/tag/publish work stopped until manual testing is complete and the
  owner explicitly asks for a release

## Runbooks

### Add a collector

1. Install Beacon on the collector machine.
2. Create or edit `~/.beacon/beacon.toml` with `[fleet].role = "collector"`,
   `[fleet].control_plane_url`, `[fleet].node_name`, and the local
   `[[capture.sources]]`.
3. On the control plane, run `beacon init --enroll-ttl 30m`.
4. On the collector, run `beacon enroll https://beacon.example.com
   --token-stdin` or `--token-env BEACON_ENROLL_TOKEN`.
5. Run `beacon collect --once`.
6. Check `beacon status` on the control plane and confirm the dashboard shows
   the new node/source filters.
7. Start the collector as a supervised service.

### Rotate a collector ingest token

1. Leave the existing `[fleet].ingest_token_file` on the collector.
2. On the control plane, run `beacon init --enroll-ttl 30m`.
3. On the collector, rerun `beacon enroll https://beacon.example.com
   --token-stdin` or `--token-env BEACON_ENROLL_TOKEN`.
4. Confirm the command writes a new ingest token file and preserves the same
   node/collector assignment.
5. Restart `beacon collect`.

### Control plane outage

1. Leave collectors running unless the spool is full or disk pressure requires
   action.
2. Restore the control plane, ClickHouse, and reverse proxy.
3. Run `beacon status`.
4. Watch collector logs. They should retry and send pending batches after the
   control plane is reachable.
5. If a collector reports `collector spool is full`, increase
   `[fleet].spool_max_bytes`, free disk space, or restore connectivity before
   deleting source files that have not been acknowledged.

### Collector disk pressure

1. Stop the collector process.
2. Check `[fleet].spool_dir`, especially `pending`, `inflight`, and
   `quarantine`.
3. Do not delete healthy pending or inflight batches unless you are intentionally
   abandoning unacknowledged captured data.
4. Restore control-plane connectivity or increase `[fleet].spool_max_bytes`.
5. Restart `beacon collect`.
6. Verify pending files drain and dashboard activity resumes.

### Corrupt spool

Use the detailed corrupt spool runbook in [Errors and observability](errors.md).
The short version is: stop the collector, preserve quarantined JSON until source
files are confirmed rereadable, move damaged files out of the spool tree, keep
`collector-state.json` unless intentionally replaying, then restart collection.

### Destructive reset and replay

Use this when you intentionally want to drop Beacon-owned ClickHouse data and
rebuild from source files.

1. Stop remote collectors or leave them running only if you understand they will
   pause/reject old-epoch writes during reset.
2. On the control plane, run:

   ```bash
   beacon db reset --force
   ```

3. Run:

   ```bash
   beacon status
   ```

   Confirm `reset_pending=false` and note the advanced `schema_epoch`.
4. Keep or restart the control plane with `beacon up`.
5. Create a fresh enrollment token with `beacon init --enroll-ttl 30m`.
6. Re-enroll each collector so it receives a current-epoch ingest token.
7. Restart `beacon collect` on each collector.
8. Confirm activity reappears after replay/backfill.

Reset does not delete original agent session files or the control-plane metadata
database. It drops and recreates Beacon-owned ClickHouse tables. If the reset
fails, `reset_pending` intentionally remains active so stale collectors do not
write into the new epoch.

### Remote MCP cannot read data

1. Verify the control plane is reachable:

   ```bash
   curl -fsS https://beacon.example.com/health
   ```

2. Verify the MCP endpoint and token:

   ```bash
   printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"ping"}' | \
     env BEACON_MCP_URL=https://beacon.example.com/api/mcp \
         BEACON_READ_TOKEN="$BEACON_READ_TOKEN" \
         beacon mcp
   ```

3. Confirm the token has read scope and is not restricted to a different node,
   collector, or source.
4. If local/direct ClickHouse mode is being used, switch to remote MCP mode for
   normal cross-machine workflows.

### Dashboard is reachable without auth

1. Check `[server].host`.
2. If it is non-loopback, check `[auth].mode`.
3. Prefer `reverse-proxy` behind a trusted TLS proxy.
4. If using `owner-token`, confirm non-loopback access is over HTTPS or a
   trusted tunnel. Do not leave `allow_insecure_owner_http = true` on an exposed
   network.
5. Restart `beacon up` after config changes.

## Manual-test handoff

Before marking the multi-machine dashboard goal ready for manual testing:

1. Complete all implementation issues through #248.
2. Run the final holistic review issue (#249).
3. Run the broad validation listed in tracker #237.
4. Rebuild and install the local dev binary:

   ```bash
   make install-local INSTALL_DIR="$HOME/.local/bin"
   ```

5. Restart the local review server:

   ```bash
   tmux kill-session -t beacon-up 2>/dev/null || true
   tmux new-session -d -s beacon-up "$HOME/.local/bin/beacon up"
   curl -fsS http://localhost:4600/ >/dev/null
   ```

6. Hand off the review URL and validation evidence.
7. Do not cut, tag, publish, or prepare a patch release until the owner manually
   tests the build and explicitly asks for a release.
