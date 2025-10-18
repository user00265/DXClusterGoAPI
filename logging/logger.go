package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
	colorGray   = "\033[90m"
	colorWhite  = "\033[97m"
)

// Log levels
const (
	LevelCrit   = iota // 0 - Critical errors (fatal, app should stop)
	LevelError         // 1 - Errors (non-fatal but important)
	LevelWarn          // 2 - Warnings
	LevelNotice        // 3 - Important info (startup, shutdown, config)
	LevelInfo          // 4 - General info
	LevelDebug         // 5 - Debug details
)

var (
	// Logger is the package-level logger used across the project.
	Logger = log.New(os.Stdout, "", 0) // We'll handle our own formatting
	// Level controls verbosity. Default to NOTICE for sane production defaults.
	Level      = LevelNotice
	UseColors  = true                  // Enable colors by default
	TimeFormat = "Jan 02 15:04:05.000" // Kyle's format: "Oct 14 13:16:37.788"
)

// SetLevel sets the logger verbosity level.
func SetLevel(l int) {
	Level = l
}

// SetOutput sets the output destination for logs
func SetOutput(w io.Writer) {
	Logger.SetOutput(w)
}

// DisableColors disables color output
func DisableColors() {
	UseColors = false
}

// formatLog formats a log message with timestamp, colored level (3-letter), and message
// Sanitizes the message to prevent log injection (e.g., from user-controlled input like DX cluster data)
func formatLog(levelAbbrev, color, message string) string {
	timestamp := time.Now().Format(TimeFormat)

	// Sanitize message: escape newlines and carriage returns to prevent log forging
	// Replace with visible escape sequences so data is not lost, but injection is prevented
	sanitized := sanitizeLogMessage(message)

	if UseColors {
		// Format: "Oct 14 13:16:37.788 INF message"
		return fmt.Sprintf("%s %s%s%s %s", timestamp, color, levelAbbrev, colorReset, sanitized)
	}

	// No colors
	return fmt.Sprintf("%s %s %s", timestamp, levelAbbrev, sanitized)
}

// sanitizeLogMessage escapes problematic characters in log messages to prevent log injection
// Specifically escapes newlines and carriage returns that could forge new log entries
func sanitizeLogMessage(msg string) string {
	// Replace problematic characters with visible escape sequences
	result := strings.NewReplacer(
		"\n", "\\n",
		"\r", "\\r",
		"\t", "\\t",
	).Replace(msg)
	return result
}

// Crit logs critical errors (application should stop)
func Crit(format string, v ...interface{}) {
	if Level >= LevelCrit {
		msg := fmt.Sprintf(format, v...)
		Logger.Print(formatLog("CRT", colorRed, msg))
	}
}

// Error logs error-level messages (non-fatal but important)
func Error(format string, v ...interface{}) {
	if Level >= LevelError {
		msg := fmt.Sprintf(format, v...)
		Logger.Print(formatLog("ERR", colorRed, msg))
	}
}

// Warn logs warning-level messages
func Warn(format string, v ...interface{}) {
	if Level >= LevelWarn {
		msg := fmt.Sprintf(format, v...)
		Logger.Print(formatLog("WRN", colorYellow, msg))
	}
}

// Notice logs important informational messages (startup, config, shutdown)
func Notice(format string, v ...interface{}) {
	if Level >= LevelNotice {
		msg := fmt.Sprintf(format, v...)
		Logger.Print(formatLog("NOT", colorCyan, msg))
	}
}

// Info logs general informational messages
func Info(format string, v ...interface{}) {
	if Level >= LevelInfo {
		msg := fmt.Sprintf(format, v...)
		Logger.Print(formatLog("INF", colorWhite, msg))
	}
}

// Debug logs very verbose diagnostic messages
func Debug(format string, v ...interface{}) {
	if Level >= LevelDebug {
		msg := fmt.Sprintf(format, v...)
		Logger.Print(formatLog("DBG", colorGray, msg))
	}
}
