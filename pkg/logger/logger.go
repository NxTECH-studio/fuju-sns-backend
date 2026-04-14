// Package logger provides logging functionality for the application.
package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// Level represents log level
type Level int

// Level represents the log level.
const (
	DebugLevel Level = iota
	InfoLevel
	WarnLevel
	ErrorLevel
)

// Logger provides structured JSON logging
type Logger struct {
	level  Level
	writer io.Writer
}

// Entry represents a log entry
type Entry struct {
	Timestamp string                 `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
	RequestID string                 `json:"request_id,omitempty"`
	UserID    interface{}            `json:"user_id,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

// New creates a new logger
func New(level string) *Logger {
	var logLevel Level

	switch level {
	case "debug":
		logLevel = DebugLevel
	case "info":
		logLevel = InfoLevel
	case "warn":
		logLevel = WarnLevel
	case "error":
		logLevel = ErrorLevel
	default:
		logLevel = InfoLevel
	}

	return &Logger{
		level:  logLevel,
		writer: os.Stdout,
	}
}

// NewWithWriter creates a new logger with custom writer
func NewWithWriter(level string, writer io.Writer) *Logger {
	var logLevel Level

	switch level {
	case "debug":
		logLevel = DebugLevel
	case "info":
		logLevel = InfoLevel
	case "warn":
		logLevel = WarnLevel
	case "error":
		logLevel = ErrorLevel
	default:
		logLevel = InfoLevel
	}

	return &Logger{
		level:  logLevel,
		writer: writer,
	}
}

// Debug logs a debug message
func (l *Logger) Debug(ctx context.Context, message string, fields ...interface{}) {
	if l.level <= DebugLevel {
		l.log(ctx, "DEBUG", message, fields...)
	}
}

// Info logs an info message
func (l *Logger) Info(ctx context.Context, message string, fields ...interface{}) {
	if l.level <= InfoLevel {
		l.log(ctx, "INFO", message, fields...)
	}
}

// Warn logs a warn message
func (l *Logger) Warn(ctx context.Context, message string, fields ...interface{}) {
	if l.level <= WarnLevel {
		l.log(ctx, "WARN", message, fields...)
	}
}

// Error logs an error message
func (l *Logger) Error(ctx context.Context, message string, err error, fields ...interface{}) {
	if l.level <= ErrorLevel {
		fieldMap := fieldsToMap(fields...)
		l.logWithError(ctx, "ERROR", message, err, fieldMap)
	}
}

// log logs a message with fields
func (l *Logger) log(ctx context.Context, level, message string, fields ...interface{}) {
	fieldMap := fieldsToMap(fields...)

	entry := &Entry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     level,
		Message:   message,
		Fields:    fieldMap,
		RequestID: getRequestID(ctx),
		UserID:    getUserID(ctx),
	}

	l.writeEntry(entry)
}

// logWithError logs a message with error
func (l *Logger) logWithError(ctx context.Context, level, message string, err error, fieldMap map[string]interface{}) {
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}

	entry := &Entry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     level,
		Message:   message,
		Fields:    fieldMap,
		RequestID: getRequestID(ctx),
		UserID:    getUserID(ctx),
		Error:     errMsg,
	}

	l.writeEntry(entry)
}

// writeEntry writes a log entry as JSON
func (l *Logger) writeEntry(entry *Entry) {
	data, err := json.Marshal(entry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal log entry: %v\n", err)
		return
	}

	_, _ = fmt.Fprintln(l.writer, string(data))
}

// fieldsToMap converts variadic fields to a map
// Expected format: "key1", value1, "key2", value2, ...
func fieldsToMap(fields ...interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for i := 0; i < len(fields); i += 2 {
		if i+1 >= len(fields) {
			break
		}

		key, ok := fields[i].(string)
		if !ok {
			continue
		}

		result[key] = fields[i+1]
	}

	return result
}

// Context key for request ID
type contextKey string

const (
	requestIDKey contextKey = "request_id"
	userIDKey    contextKey = "user_id"
)

// WithRequestID adds request ID to context
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// WithUserID adds user ID to context
func WithUserID(ctx context.Context, userID interface{}) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// getRequestID retrieves request ID from context
func getRequestID(ctx context.Context) string {
	if requestID := ctx.Value(requestIDKey); requestID != nil {
		return fmt.Sprint(requestID)
	}
	return ""
}

// getUserID retrieves user ID from context
func getUserID(ctx context.Context) interface{} {
	return ctx.Value(userIDKey)
}
