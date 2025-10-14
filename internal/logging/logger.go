package logging

import (
	"log"
	"os"
)

// Log levels (higher number = more verbose)
const (
	LevelError = iota // 0
	LevelWarn         // 1
	LevelInfo         // 2
	LevelDebug        // 3
)

var (
	// Logger is the package-level logger used across the project.
	Logger = log.New(os.Stdout, "", log.LstdFlags)
	// Level controls verbosity. Default to WARN for sane production defaults.
	Level = LevelWarn
)

// SetLevel sets the logger verbosity level.
func SetLevel(l int) {
	Level = l
}

// Error logs error-level messages (always important).
func Error(format string, v ...interface{}) {
	if Level >= LevelError {
		Logger.Printf("| ERROR | "+format, v...)
	}
}

// Warn logs warning-level messages.
func Warn(format string, v ...interface{}) {
	if Level >= LevelWarn {
		Logger.Printf("| WARN  | "+format, v...)
	}
}

// Info logs informational messages (less noisy).
func Info(format string, v ...interface{}) {
	if Level >= LevelInfo {
		Logger.Printf("| INFO  | "+format, v...)
	}
}

// Debug logs very verbose diagnostic messages.
func Debug(format string, v ...interface{}) {
	if Level >= LevelDebug {
		Logger.Printf("| DEBUG | "+format, v...)
	}
}
