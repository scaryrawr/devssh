# Agent Development Guide

This document provides coding guidelines and conventions for AI agents
working on the devssh project.

## Project Overview

`devssh` is a Go CLI that wraps `ssh` with port-forwarding, browser-opening,
and desktop-notification helpers. It is a fork of `gh-ado-codespaces` with
all GitHub CLI / Azure DevOps machinery removed; the only external command
it shells out to is `ssh`.

The dominant runtime pattern is a single OpenSSH `ControlMaster` started at
session start and used by every subsequent operation (script upload, port
monitor, dynamic port forwards, interactive shell). The `Mux` type in
`sshmux.go` owns the master's lifecycle.

## Build, Test, and Lint Commands

### Build

```bash
go build -v .
go build -v -o devssh .
```

### Test

```bash
go test -v ./...                # all tests
go test -v -race .              # race detector (CI standard)
go test -short -v ./...         # skip integration tests
go test -v -run TestFunctionName
go test -v -cover ./...
```

### Format and Lint

```bash
go fmt ./...
go vet ./...
golangci-lint run               # if installed
```

## Code Style Guidelines

Standard Go idioms. Highlights:

- Tabs for indentation (`gofmt`).
- Imports grouped: stdlib, third-party, local — separated by blank lines.
- PascalCase for exported, camelCase for unexported. Acronyms keep
  consistent casing (`HTTP`, `URL`, `ID` at start of identifier; `http`,
  `url`, `id` mid-identifier).
- Always check errors; wrap with `fmt.Errorf("context: %w", err)`. Never
  ignore an error with `_`.
- Pass `context.Context` as the first argument to any function that may
  block. Respect cancellation in loops.
- Use `defer` for cleanup. Use `sync.WaitGroup` and channels for goroutine
  coordination.
- Write doc comments for every exported identifier, starting with the
  identifier name and forming a complete sentence.
- Keep functions short (~50 lines) and focused. Use early returns.

## Logging

- `logDebug(format, args...)` writes to the session debug log at
  `$TMPDIR/devssh/logs/<session>/devssh.log`. The logger is set up by
  `initDebugLogger()` after the session ID is initialized.
- `fmt.Fprintf(os.Stderr, ...)` for user-facing warnings and progress.
- Never log secrets or full tokens (we don't handle any, but be mindful
  if you add new features).

## Project-Specific Patterns

### Mux (ControlMaster wrapper)

Every interaction with the remote goes through `*Mux`:

- `mux.Run(ctx, args...)` — capture stdout/stderr from a remote command.
- `mux.Command(ctx, args...)` — build an `*exec.Cmd` for streaming
  (used by the port monitor).
- `mux.AddLocalForward(local, remote)` / `mux.CancelLocalForward` —
  dynamic `-L` forwards on the live master.
- `mux.AddRemoteForward(remoteSpec, localSpec)` /
  `mux.CancelRemoteForward` — dynamic `-R` forwards, including
  streamlocal Unix sockets.
- `mux.InteractiveShell(ctx, extraArgs)` — the final interactive ssh.
- `mux.Stop()` — `ssh -O exit` + socket cleanup. Safe to call twice.

User-supplied SSH flags (everything after `--`) are appended only to
`InteractiveShell`. Internal commands always use `BatchMode=yes` and a
known-good baseline of `-S socket -o ControlMaster=no
-o ControlPath=socket`.

### Helper scripts

Embedded via `//go:embed`:

- `port-monitor.sh` → `port-monitor.go`
- `browser-opener.sh` → `browser.go`
- `notification-sender.sh` → `notification.go`
- `xdg-open.sh` → `bash.go`

Socket file names on the remote follow the pattern
`/tmp/devssh-<service>-<uuid>.sock`. Both the Go services and the shell
scripts agree on this prefix.

### Session logs

- All log files live under
  `getSessionLogDirectory()` = `$TMPDIR/devssh/logs/<session-id>/`.
- `sessionID` is set once via `initializeSessionID(hostAlias)` from
  the resolved host name + timestamp + pid.

### Configuration

- Per-host overrides are keyed by the SSH alias (not the resolved
  hostname or `user@host`).
- `cfg.ReversePortForwardsForHost(host)` returns the merged forward
  list. Order: built-in `WellKnownPorts` → top-level `reversePortForward`
  → host-specific overrides.

## Common Pitfalls to Avoid

- Do not use `cd` to switch working directories in commands; pass a path
  explicitly or set `cmd.Dir`.
- Do not ignore context cancellation in loops.
- Do not forget to close listeners, files, and `*exec.Cmd` stdio pipes.
- Do not call `kevinburke/ssh_config`'s package-level `Get()` — it uses
  a singleton parsed from the real `~/.ssh/config`. Always parse with
  `ssh_config.Decode()` and use the returned `*Config.Get(alias, key)`.
- Do not use `panic` for error handling; return errors.
- Do not modify global state (e.g. `WellKnownPorts`) without
  synchronization. Tests that mutate it must restore the original.
