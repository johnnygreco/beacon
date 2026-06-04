# Beacon Agent Instructions

## Keep the Local Dev Build Served

Unless the user explicitly tells you otherwise, this machine should always be
serving the latest dev version of Beacon for human review.

After making Beacon updates, rebuild and install the workspace binary locally:

```bash
make install-local INSTALL_DIR="$HOME/.local/bin"
```

Then restart the persistent review server from that installed binary:

```bash
tmux kill-session -t beacon-up 2>/dev/null || true
tmux new-session -d -s beacon-up "$HOME/.local/bin/beacon up"
```

Verify that the server started and is responding before handing work back:

```bash
tmux capture-pane -pt beacon-up -S -80
curl -fsS http://localhost:4600/ >/dev/null
```

If the configured web port is not `4600`, use the configured port instead.
Report the review URL to the user. If a change affects UI behavior, also verify
the served dashboard visually against this local dev server.
