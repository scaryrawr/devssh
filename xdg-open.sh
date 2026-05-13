#!/usr/bin/env bash
# xdg-open shim for GitHub Codespaces (SSH environment)
#
# Replaces the real xdg-open and intelligently routes SSH open requests:
#   - URLs  → forwarded via devssh browser socket, $BROWSER, VS Code, or silent no-op
#   - Files → opened with an appropriate viewer (chafa, pdftotext, glow, bat, $EDITOR…)
#             in a tmux pane (if available) or inline over SSH.
# Outside SSH, immediately delegates to the real xdg-open.
#
# Anti-recursion: this script never calls the first "xdg-open" from PATH.
# When delegating to the real binary, it searches for an executable named
# xdg-open that is not this shim (including symlinks), with /usr/bin and /bin
# as final fallbacks.

set -euo pipefail

# has_ssh_connection: returns 0 when running under an SSH session.
has_ssh_connection() {
    [[ -n "${SSH_CONNECTION:-}" || -n "${SSH_TTY:-}" || -n "${SSH_CLIENT:-}" ]]
}

# find_real_xdg_open: print the first xdg-open executable that is not this shim.
find_real_xdg_open() {
    local self="${BASH_SOURCE[0]}"
    if [[ "$self" != */* ]]; then
        self="$(type -P "$self" 2>/dev/null || printf '%s' "$self")"
    fi

    local candidate
    while IFS= read -r candidate; do
        [[ -z "$candidate" || ! -x "$candidate" ]] && continue
        if [[ -e "$self" && "$candidate" -ef "$self" ]]; then
            continue
        fi
        printf '%s\n' "$candidate"
        return 0
    done < <(
        type -P -a xdg-open 2>/dev/null || true
        printf '%s\n' /usr/bin/xdg-open /bin/xdg-open
    )

    return 1
}

# real_xdg_open: call the system xdg-open without recursing into this shim.
real_xdg_open() {
    local real
    real="$(find_real_xdg_open)" || return 1
    "$real" "$@"
}

# exec_real_xdg_open: replace this shim with the system xdg-open.
exec_real_xdg_open() {
    local real
    real="$(find_real_xdg_open)" || return 1
    exec "$real" "$@"
}

# Outside SSH, behave exactly like the system xdg-open instead of applying any
# devssh-specific URL/file handling.
if ! has_ssh_connection; then
    exec_real_xdg_open "$@" || exit $?
fi

TARGET="${1:-}"

if [[ -z "$TARGET" ]]; then
    echo "Usage: $(basename "$0") <url-or-file>" >&2
    exit 1
fi

# ---------------------------------------------------------------------------
# URL handling
# ---------------------------------------------------------------------------

# open_url: try multiple strategies to open a URL, falling back gracefully.
open_url() {
    local url="$1"

    # 1. Try the devssh browser socket (devssh port-forwarding service).
    #    Mirror the exact discovery pattern used by browser-opener.sh.
    if command -v curl &>/dev/null && command -v jq &>/dev/null; then
        local encoded_url
        encoded_url="$(printf %s "$url" | jq -sRr @uri)"

        # Find all sockets, sort newest-first so we prefer the active one.
        local sock
        while IFS= read -r sock; do
            [[ -z "$sock" ]] && continue
            if curl -s --max-time 2 --unix-socket "$sock" \
                    -X POST "http://localhost/open?url=${encoded_url}" \
                    >/dev/null 2>&1; then
                return 0
            fi
        done < <(find /tmp -maxdepth 1 -name "devssh-browser-*.sock" -type s \
                     -exec ls -t {} + 2>/dev/null)
    fi

    # 2. If $BROWSER is set, delegate to it.
    if [[ -n "${BROWSER:-}" ]]; then
        "$BROWSER" "$url" && return 0
    fi

    # 3. Try VS Code remote's `code --open-url`.
    if command -v code &>/dev/null; then
        code --open-url "$url" &>/dev/null && return 0
    fi

    # 4. Try the real xdg-open.
    real_xdg_open "$url" &>/dev/null && return 0

    # 5. Silent no-op — headless environment, nothing else we can do.
    return 0
}

# ---------------------------------------------------------------------------
# File handling
# ---------------------------------------------------------------------------

# detect_viewer: sets the global VIEWER_CMD array to the command + args
# needed to view the given file. Uses an array to avoid eval/injection risks.
detect_viewer() {
    local file="$1"
    local ext="${file##*.}"
    ext="${ext,,}"  # lowercase

    case "$ext" in
        jpg|jpeg|png|gif|bmp|webp|tiff|svg)
            if command -v chafa &>/dev/null; then
                VIEWER_CMD=(chafa "$file")
                return
            fi
            ;;
        pdf)
            if command -v pdftotext &>/dev/null; then
                VIEWER_CMD=(bash -c "pdftotext $(printf '%q' "$file") - | less")
                return
            elif command -v pdfinfo &>/dev/null; then
                VIEWER_CMD=(pdfinfo "$file")
                return
            fi
            ;;
        md|markdown)
            if command -v glow &>/dev/null; then
                VIEWER_CMD=(glow "$file")
                return
            elif command -v bat &>/dev/null; then
                VIEWER_CMD=(bat "$file")
                return
            fi
            ;;
    esac

    # Everything else: use $EDITOR or fall back to vi.
    if [[ -n "${EDITOR:-}" ]]; then
        VIEWER_CMD=("$EDITOR" "$file")
    else
        VIEWER_CMD=(vi "$file")
    fi
}

# is_interactive_editor: returns 0 if VIEWER_CMD is a known interactive editor.
# Checks the executable name only (first element of the array).
is_interactive_editor() {
    local exe
    exe="$(basename "${VIEWER_CMD[0]}")"
    case "$exe" in
        vi|vim|nvim|nano|emacs|emacsclient|micro|hx|helix|kak|kakoune)
            return 0
            ;;
    esac
    return 1
}

# open_file: open a file using the best available strategy.
open_file() {
    local file="$1"

    if [[ ! -e "$file" ]]; then
        echo "$(basename "$0"): '$file': No such file or directory" >&2
        exit 2
    fi

    # VIEWER_CMD is set as a global array by detect_viewer.
    VIEWER_CMD=()
    detect_viewer "$file"

    if [[ -n "${TMUX:-}" ]]; then
        # Inside a tmux session → open in a vertical split pane.
        if is_interactive_editor; then
            # Editors are fully interactive; run them directly.
            tmux split-window -h "${VIEWER_CMD[@]}"
        else
            # Non-interactive viewers (chafa, bat, less…): keep the pane open
            # until the user presses Enter so they can read the output.
            # We pass the command as a quoted string to bash -c so that tmux
            # receives a single string argument (no eval of user-controlled data).
            local quoted_cmd
            printf -v quoted_cmd '%q ' "${VIEWER_CMD[@]}"
            tmux split-window -h "bash -c ${quoted_cmd@Q}; read -r -p 'Press enter to close...'"
        fi

    else
        # SSH session without tmux → run viewer inline (blocking).
        "${VIEWER_CMD[@]}"
    fi
}

# ---------------------------------------------------------------------------
# Main dispatch
# ---------------------------------------------------------------------------

# Detect whether the target looks like a URL (http, https, mailto, ftp).
# file:// URLs are routed to open_file after stripping the scheme prefix.
if [[ "$TARGET" =~ ^(https?|mailto|ftp):// ]]; then
    open_url "$TARGET"
elif [[ "$TARGET" =~ ^file:// ]]; then
    # Strip the file:// prefix (and optional localhost authority) to get the path.
    file_path="${TARGET#file://}"
    file_path="${file_path#localhost}"
    open_file "$file_path"
else
    open_file "$TARGET"
fi
