# Command Completion Notifications

[← Back to README](../README.md)

devssh can surface desktop notifications when long-running remote commands
finish, inspired by the [done](https://github.com/franciscolourenco/done)
project.

## How it works

1. devssh starts a local HTTP service backed by `gen2brain/beeep`, which
   posts to your OS's native notification center.
2. The service is reverse-forwarded into the remote as a Unix socket at
   `/tmp/devssh-notification-<uuid>.sock`.
3. `~/notification-sender.sh` is uploaded to the remote. It provides
   bash/zsh hooks and a `send <title> <message>` subcommand for other
   shells.

## Enabling it on the remote

**Bash or Zsh** — add to `~/.bashrc` / `~/.zshrc`:

```bash
if [ -f "$HOME/notification-sender.sh" ]; then
    source "$HOME/notification-sender.sh"
fi
```

**Fish** — pair with the [`done`](https://github.com/franciscolourenco/done)
plugin in `~/.config/fish/config.fish`:

```fish
set -U __done_allow_nongraphical 1
set -U __done_notification_command "~/notification-sender.sh send \$title \$message"
set -U __done_min_cmd_duration 5000
```

Install `done` via Fisher: `fisher install franciscolourenco/done`.

## Configuration

```bash
# Minimum command duration in seconds before triggering a notification (default 5).
export NOTIFICATION_MIN_DURATION=10
```

Notifications include command status (completed/failed), the command
itself, duration, and exit code.

## Supported platforms

- **macOS** — native notification center
- **Linux** — `notify-send` (via D-Bus)
- **Windows** — Windows notification system

The remote needs `jq` and `curl` for the sender script. Without them, the
hooks are no-ops.
