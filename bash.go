package devssh

import (
	_ "embed"
	"strings"
)

//go:embed xdg-open.sh
var xdgOpenScript string

// wrapBashLoginCommand wraps a shell command for execution under `bash -lc`,
// safely single-quoting the command so it survives transport through ssh.
func wrapBashLoginCommand(command string) []string {
	return []string{"bash", "-lc", quoteForShell(command)}
}

// quoteForShell returns command wrapped in single quotes, escaping any
// embedded single quotes using the standard `'"'"'` idiom.
func quoteForShell(command string) string {
	if command == "" {
		return "''"
	}

	return "'" + strings.ReplaceAll(command, "'", `'"'"'`) + "'"
}
