# Testing

[← Back to README](../README.md)

devssh's test suite covers each module independently. No live SSH server
is needed — the `Mux` tests stub out `ssh` via a `PATH` shim.

## Test files

- `args_test.go` — command-line parsing, the `--` separator, positional
  vs. flag args.
- `bash_test.go` — shell quoting helpers.
- `browser_test.go` — local HTTP browser service lifecycle and request
  validation.
- `config_test.go` — config file loading/saving, per-host override merge
  semantics, env-var path override.
- `host_test.go` — `~/.ssh/config` parsing (Include, multi-pattern hosts,
  wildcard exclusion, hostname/user/port resolution).
- `notification_test.go` — local HTTP notification service lifecycle,
  JSON validation, truncation.
- `port_test.go` — reverse port forward merging, well-known port table,
  bound-port detection.
- `sshmux_test.go` — `Mux` command construction via a fake `ssh` binary
  installed on `PATH`.

## Running tests

```bash
# All tests
go test -v ./...

# CI-style with race detector
go test -v -race .

# Skip integration tests
go test -short -v ./...

# Single test
go test -v -run TestStartMux_BuildsExpectedCommand

# Coverage
go test -v -cover ./...
```

The suite is fully self-contained and does not require:

- A live SSH server.
- The actual GitHub CLI (`gh`).
- Cloud credentials of any kind.
