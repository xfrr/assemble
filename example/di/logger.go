package di

import (
	"log/slog"
)

// SimpleLogger is a simple implementation of Logger.
// Just for demonstration purposes.
type SimpleLogger struct {
	slogger *slog.Logger
}

func NewSimpleLogger() (SimpleLogger, error) {
	return SimpleLogger{slogger: slog.Default()}, nil
}

func (l SimpleLogger) Info(msg string, args ...any) {
	l.slogger.Info(msg, args...)
}

func (l SimpleLogger) Infof(msg string, args ...any) {
	l.slogger.Info(msg, args...)
}
