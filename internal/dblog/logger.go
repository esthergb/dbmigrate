package dblog

import (
	"io"
	"log/slog"
)

type Logger struct {
	inner *slog.Logger
}

func New(w io.Writer, json bool, verbose bool) *Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if json {
		handler = slog.NewJSONHandler(w, opts)
	} else {
		handler = slog.NewTextHandler(w, opts)
	}

	return &Logger{inner: slog.New(handler)}
}

func NewNop() *Logger {
	return New(io.Discard, false, false)
}

func (l *Logger) Debug(msg string, args ...any) {
	l.inner.Debug(msg, args...)
}

func (l *Logger) Info(msg string, args ...any) {
	l.inner.Info(msg, args...)
}

func (l *Logger) Warn(msg string, args ...any) {
	l.inner.Warn(msg, args...)
}

func (l *Logger) Error(msg string, args ...any) {
	l.inner.Error(msg, args...)
}

func (l *Logger) With(args ...any) *Logger {
	return &Logger{inner: l.inner.With(args...)}
}
