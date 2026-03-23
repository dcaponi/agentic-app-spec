package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Logger provides structured logging with a component tag.
type Logger struct {
	Component string
}

// NewLogger creates a logger tagged with the given component name.
func NewLogger(component string) *Logger {
	return &Logger{Component: component}
}

func (l *Logger) log(level, msg string, data ...map[string]interface{}) {
	ts := time.Now().UTC().Format(time.RFC3339)
	extra := ""
	if len(data) > 0 && data[0] != nil {
		b, err := json.Marshal(data[0])
		if err == nil {
			extra = " " + string(b)
		}
	}
	fmt.Fprintf(os.Stderr, "[%s] [%s] [%s] %s%s\n", ts, level, l.Component, msg, extra)
}

// Info logs an informational message.
func (l *Logger) Info(msg string, data ...map[string]interface{}) {
	l.log("INFO", msg, data...)
}

// Warn logs a warning message.
func (l *Logger) Warn(msg string, data ...map[string]interface{}) {
	l.log("WARN", msg, data...)
}

// Error logs an error message.
func (l *Logger) Error(msg string, data ...map[string]interface{}) {
	l.log("ERROR", msg, data...)
}

// Debug logs a debug message.
func (l *Logger) Debug(msg string, data ...map[string]interface{}) {
	l.log("DEBUG", msg, data...)
}
