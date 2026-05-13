# devssh

A pure-SSH developer-experience wrapper. `devssh` opens a multiplexed
OpenSSH session to any host and layers on top:

- 🔌 **Automatic port forwarding** — when applications on the remote
  start listening, the local side forwards their ports automatically.
- 🌐 **Browser opening** — `xdg-open` / `$BROWSER` in the remote opens
  links in your local browser.
- 🔔 **Desktop notifications** — long-running remote commands can ping
  your local notification center when they finish.
- 🔁 **Reverse forwards for local AI services** — Ollama (`11434`),
  LM Studio (`1234`), and Chrome DevTools (`9222`) on your machine are
  reverse-forwarded into the remote when they're running.
- 🪟 **`xdg-open` shim** — an intelligent open-anything wrapper for
  remote sessions (chafa, glow, bat, tmux split, …).

It's a fork of [`gh-ado-codespaces`][gh-ado-codespaces] with the GitHub
CLI and Azure DevOps auth machinery stripped out. Pure `ssh`, anywhere.

[gh-ado-codespaces]: https://github.com/scaryrawr/gh-ado-codespaces

## Requirements

**Local:**
- OpenSSH client with multiplexing support (`ControlMaster=auto` + `-O check`).
- Linux, macOS, or Windows (with OpenSSH).

**Remote (best supported):**
- Linux with `bash`, `jq`, `ss`, `curl`, `base64`, `chmod` (most of these
  are present by default on Ubuntu / Debian / Fedora images).
- `sshd` with `AllowStreamLocalForwarding yes` if you want browser and
  notification socket forwarding (the default on most distros).
- Passwordless `sudo` is **only** required if you opt in to the
  system-wide `xdg-open` symlink (see `--install-xdg-open`).

Missing remote tools degrade individual features gracefully — the SSH
session itself doesn't depend on any of them.

## Installation

Pre-built binaries are published on the [releases page](https://github.com/scaryrawr/devssh/releases).
Drop the binary somewhere on your `PATH`.

From source:

```bash
go install github.com/scaryrawr/devssh@latest
```

## Usage

```text
devssh [flags] [host] [-- ssh-flags-or-remote-command...]
```

- If `[host]` is omitted, `devssh` parses `~/.ssh/config` and shows a
  picker of concrete `Host` aliases (wildcards skipped).
- Anything after `--` is passed straight to the interactive `ssh`
  invocation — useful for ad-hoc `-L`/`-R` forwards or running a remote
  command.

```bash
# Pick a host interactively from ~/.ssh/config
devssh

# Connect to a specific host
devssh my-dev-box

# Add an extra local forward for this session only
devssh my-dev-box -- -L 3000:localhost:3000

# Run a remote command (skip the interactive shell)
devssh my-dev-box -- htop
```

### Flags

| Flag | Description |
|---|---|
| `--logs` | List recent log sessions and exit |
| `--install-xdg-open` | Also symlink `~/xdg-open.sh` into `/usr/local/bin` on the remote (needs passwordless `sudo`) |
| `--no-xdg-open` | Skip uploading and installing the `xdg-open` shim |
| `--no-port-monitor` | Disable the remote port monitor |
| `--no-browser` | Disable the local browser-opener service |
| `--no-notifications` | Disable the local notification service |
| `-v`, `--verbose` | Print extra diagnostics to stderr |

## Configuration

Optional configuration file, defaulting to:

```text
$OS_CONFIG_DIR/devssh/config.json
```

(`$OS_CONFIG_DIR` is `~/.config` on Linux, `~/Library/Application Support`
on macOS, `%AppData%` on Windows.) Override the path with the
`DEVSSH_CONFIG` environment variable.

```json
{
  "reversePortForward": [
    { "port": 8081, "description": "Custom service", "enabled": true }
  ],
  "installXdgOpen": false,
  "hosts": {
    "my-dev-box": {
      "installXdgOpen": true,
      "reversePortForward": [
        { "port": 9090, "description": "Host-only service", "enabled": true }
      ]
    }
  }
}
```

- `reversePortForward` entries are merged in this order: built-in defaults
  → top-level config → per-host overrides. Later entries override earlier
  ones by port number.
- devssh installs the `xdg-open` shim into `~/.local/bin/xdg-open` by
  default. Ensure `~/.local/bin` is on the remote `PATH`.
- `installXdgOpen` defaults to `false`; when enabled, devssh also installs
  the shim into `/usr/local/bin/xdg-open`. Per-host overrides take precedence.

## How It Works

| Feature | Doc |
|---|---|
| Port forwarding (both directions) | [docs/port-forwarding.md](docs/port-forwarding.md) |
| Browser opening | [docs/browser-opening.md](docs/browser-opening.md) |
| Desktop notifications | [docs/notifications.md](docs/notifications.md) |
| `xdg-open` shim | [`xdg-open.sh`](xdg-open.sh) |

Under the hood, `devssh` starts a dedicated SSH ControlMaster against the
target host (`ssh -M -N -f -S <socket> host`) and routes every subsequent
operation — helper-script upload, the port monitor, dynamic `-O forward` /
`-O cancel` calls, the interactive shell — through that single
multiplexed connection. On exit, it tears the master down with
`ssh -O exit`.

## Testing

```bash
go test -v ./...
go test -v -race ./...
go test -short -v ./...
```

See [docs/testing.md](docs/testing.md) for the full overview.

## Acknowledgments

Forked from [`gh-ado-codespaces`][gh-ado-codespaces], which in turn built on
[`ado-ssh-auth`](https://github.com/scaryrawr/ado-ssh-auth).
