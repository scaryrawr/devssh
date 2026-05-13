# Browser Opening

[← Back to README](../README.md)

devssh forwards "open this URL" requests from the remote to your local
machine so links you click in remote tools open in your browser.

## How it works

1. When you connect, devssh starts a small local HTTP server that opens
   URLs in your default browser.
2. The server's TCP port is reverse-forwarded into the remote as a Unix
   socket at `/tmp/devssh-browser-<uuid>.sock` using `ssh -O forward -R
   <socket>:127.0.0.1:<port>`.
3. `~/browser-opener.sh` is uploaded to the remote. It finds the most
   recent `devssh-browser-*.sock` and POSTs the URL via `curl --unix-socket`.
4. Configure the remote to use it as the system browser by adding to your
   shell config (`~/.bashrc`, `~/.zshrc`, or `~/.config/fish/config.fish`):

   ```bash
   export BROWSER="$HOME/browser-opener.sh"
   ```

5. Any tool that consults `$BROWSER` — `python -m webbrowser`, `gh
   browse`, `xdg-open` via the shim, etc. — will then pop links open
   locally.

## xdg-open shim

devssh also uploads `~/xdg-open.sh`, a smarter wrapper that routes URLs
through the same browser socket and opens local files with an appropriate
viewer (chafa for images, glow for markdown, etc.). By default, devssh
symlinks it as `~/.local/bin/xdg-open` on the remote.

Most Linux shell profiles put `~/.local/bin` on `PATH` once the directory
exists. If `xdg-open` still resolves to the system binary, add this to your
remote shell config:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Pass `--install-xdg-open` to also symlink it as
`/usr/local/bin/xdg-open` on the remote (needs passwordless sudo).

## Use cases

- Opening documentation links from CLI tools.
- Viewing web-based dev servers running on the remote.
- OAuth flows that need to bounce through a browser.
- Anything that calls `xdg-open` or honors `$BROWSER`.
