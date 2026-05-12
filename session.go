package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// sessionID is the global session identifier set during startup.
var sessionID string

// initializeSessionID seeds the session identifier from the host alias plus a
// timestamp and pid. Safe to call once near startup.
func initializeSessionID(hostAlias string) {
	timestamp := time.Now().Format("2006-01-02_150405")
	pid := os.Getpid()

	safeName := sanitizeForFilename(hostAlias)
	if safeName == "" {
		safeName = "unknown-host"
	}

	sessionID = fmt.Sprintf("%s_session-%s-pid%d", safeName, timestamp, pid)
}

// sanitizeForFilename replaces filesystem-unsafe characters in name with
// dashes and truncates the result to 50 characters.
func sanitizeForFilename(name string) string {
	if name == "" {
		return ""
	}

	result := name
	for _, r := range []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|", " "} {
		result = strings.ReplaceAll(result, r, "-")
	}

	result = strings.Trim(result, "-")
	if len(result) > 50 {
		result = result[:50]
	}

	return result
}

// getLogDirectory returns the base temporary directory where session log
// directories live.
func getLogDirectory() string {
	return filepath.Join(os.TempDir(), "devssh", "logs")
}

// getSessionLogDirectory returns the directory for the current session.
func getSessionLogDirectory() string {
	return filepath.Join(getLogDirectory(), sessionID)
}

// getSessionLogPath returns the full path for a specific log file in the
// current session.
func getSessionLogPath(logFileName string) string {
	return filepath.Join(getSessionLogDirectory(), logFileName)
}

// ensureSessionLogDirectory creates the session log directory if missing.
func ensureSessionLogDirectory() error {
	return os.MkdirAll(getSessionLogDirectory(), 0o755)
}

// debugLogger is the shared logger for non-fatal diagnostics. It is bound to
// a file once initDebugLogger has been called; otherwise messages are
// dropped silently.
var (
	debugLogFile *os.File
	debugLogger  *log.Logger
)

// initDebugLogger initializes the debug logger writing to
// <sessionLogDir>/devssh.log. Must be called after initializeSessionID.
func initDebugLogger() error {
	if err := ensureSessionLogDirectory(); err != nil {
		return fmt.Errorf("failed to create session log directory: %w", err)
	}

	logPath := getSessionLogPath("devssh.log")

	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}

	debugLogFile = logFile
	debugLogger = log.New(logFile, "", log.LstdFlags)

	debugLogger.Printf("Debug logging initialized to %s", logPath)
	return nil
}

// logDebug appends a line to the debug log if it has been initialized.
func logDebug(format string, args ...interface{}) {
	if debugLogger != nil {
		debugLogger.Printf(format, args...)
	}
}

// closeDebugLogger closes the debug log file.
func closeDebugLogger() {
	if debugLogFile != nil {
		logDebug("Closing debug logger")
		debugLogFile.Close()
		debugLogFile = nil
		debugLogger = nil
	}
}

// ListRecentLogFiles prints a summary of recent session log directories in
// reverse chronological order.
func ListRecentLogFiles() {
	logDir := getLogDirectory()

	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		fmt.Printf("No log directory found at: %s\n", logDir)
		return
	}

	entries, err := os.ReadDir(logDir)
	if err != nil {
		fmt.Printf("Error reading log directory: %v\n", err)
		return
	}

	type sessionFile struct {
		name string
		path string
		size int64
	}
	type sessionInfo struct {
		name    string
		path    string
		modTime time.Time
		host    string
		files   []sessionFile
	}
	var sessions []sessionInfo

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		var host string
		if strings.Contains(name, "_session-") {
			parts := strings.SplitN(name, "_session-", 2)
			host = parts[0]
		} else {
			continue
		}

		sessionPath := filepath.Join(logDir, name)
		info, err := entry.Info()
		if err != nil {
			continue
		}

		sessionEntries, err := os.ReadDir(sessionPath)
		if err != nil {
			continue
		}

		var files []sessionFile
		for _, se := range sessionEntries {
			if se.IsDir() {
				continue
			}
			fileName := se.Name()
			if !strings.HasSuffix(fileName, ".log") {
				continue
			}
			fi, err := se.Info()
			if err != nil {
				continue
			}
			files = append(files, sessionFile{
				name: fileName,
				path: filepath.Join(sessionPath, fileName),
				size: fi.Size(),
			})
		}

		if len(files) > 0 {
			sessions = append(sessions, sessionInfo{
				name:    name,
				path:    sessionPath,
				modTime: info.ModTime(),
				host:    host,
				files:   files,
			})
		}
	}

	if len(sessions) == 0 {
		fmt.Printf("No session log directories found in: %s\n", logDir)
		return
	}

	// Sort newest first.
	for i := 1; i < len(sessions); i++ {
		for j := i; j > 0 && sessions[j-1].modTime.Before(sessions[j].modTime); j-- {
			sessions[j-1], sessions[j] = sessions[j], sessions[j-1]
		}
	}

	fmt.Printf("Recent log sessions in %s:\n\n", logDir)

	for _, session := range sessions {
		timeStr := session.modTime.Format("2006-01-02 15:04:05")
		fmt.Printf("Session: %s (%s) - Host: %s\n", session.name, timeStr, session.host)

		for _, f := range session.files {
			fmt.Printf("  %-20s %8s  %s\n", f.name, formatFileSize(f.size), f.path)
		}
		fmt.Println()
	}
}

func formatFileSize(bytes int64) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%d B", bytes)
	case bytes < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
	}
}
