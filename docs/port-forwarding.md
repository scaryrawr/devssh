# Port Forwarding

[← Back to README](../README.md)

devssh forwards ports in both directions over a single multiplexed SSH
connection.

## Forward Port Forwarding (Remote → Local)

When applications on the remote start listening on a port:

1. `~/port-monitor.sh` (uploaded by devssh) polls `ss -tulpn` every 2
   seconds and emits JSON events when listening ports come and go.
2. devssh reads the events from the SSH stdout and calls
   `ssh -O forward -L <port>:127.0.0.1:<port>` on the live master.
3. The remote port becomes reachable as `localhost:<port>` on your
   machine.
4. When the listener goes away, the local forward is removed with
   `ssh -O cancel -L ...`.

Ports already covered by reverse forwarding (see below) are skipped so
they don't get double-forwarded.

## Reverse Port Forwarding (Local → Remote)

Common local services are surfaced to the remote automatically:

| Port | Service | Default |
|---|---|---|
| 1234 | LM Studio | enabled |
| 9222 | Chrome DevTools | enabled |
| 11434 | Ollama | enabled |

When devssh starts it dials each port; any port that's actually bound is
reverse-forwarded into the remote (where it appears as `localhost:<port>`)
via `ssh -O forward -R <port>:127.0.0.1:<port>`.

### Customizing the list

In your devssh config (`$OS_CONFIG_DIR/devssh/config.json`):

```json
{
  "reversePortForward": [
    { "port": 8081, "description": "Custom service", "enabled": true }
  ],
  "hosts": {
    "my-dev-box": {
      "reversePortForward": [
        { "port": 9090, "description": "Host-only service", "enabled": true }
      ]
    }
  }
}
```

The merge order is: built-in defaults → top-level → per-host. Later entries
override earlier ones by port number. Set `"enabled": false` to disable a
default.

Mark an entry `"alwaysForward": true` to forward it regardless of whether
the port is bound locally at startup.

This is particularly useful for:

- Running Ollama models on your local machine while coding remotely.
- Using LM Studio's local inference server from the remote.
- Sharing any locally-running service with your remote dev environment.
