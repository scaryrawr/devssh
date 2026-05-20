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

When devssh starts it dials each configured local endpoint; any endpoint that's
actually bound is reverse-forwarded into the remote. Legacy `port` entries
appear as `localhost:<port>` via
`ssh -O forward -R <port>:127.0.0.1:<port>`.

### Customizing the list

In your devssh config (`$OS_CONFIG_DIR/devssh/config.json`):

```json
{
  "reversePortForward": [
    { "port": 8081, "description": "Same port on both sides", "enabled": true },
    { "localPort": 3000, "remotePort": 13000, "description": "Alternate remote port", "enabled": true },
    { "localPort": 8080, "remoteSocket": "/tmp/my-service-$GUID.sock", "description": "Remote socket", "enabled": true }
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
override earlier ones by local endpoint. Set `"enabled": false` to disable a
default.

Endpoint fields:

- `port` is the backward-compatible shorthand for local TCP port `N` to remote
  TCP port `N`.
- `localPort` with `remotePort` forwards a local TCP service to a different
  remote TCP port.
- `localPort` with `remoteSocket` forwards a local TCP service to a remote Unix
  socket path, for example `/tmp/my-service-$GUID.sock`.
- `$GUID` in `remoteSocket` is replaced with a per-forward UUID at startup.
- `localSocket` can be used with `remotePort` or `remoteSocket` to expose a
  local Unix socket when the local OpenSSH client supports streamlocal
  forwarding.

Mark an entry `"alwaysForward": true` to forward it regardless of whether
the local endpoint is bound at startup.

This is particularly useful for:

- Running Ollama models on your local machine while coding remotely.
- Using LM Studio's local inference server from the remote.
- Sharing any locally-running service with your remote dev environment.
