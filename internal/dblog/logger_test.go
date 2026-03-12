package dblog

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewTextLoggerInfo(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, false, false)
	l.Info("hello", "key", "val")
	out := buf.String()
	if !strings.Contains(out, "hello") {
		t.Fatalf("expected hello in output, got %q", out)
	}
	if !strings.Contains(out, "key=val") {
		t.Fatalf("expected key=val in output, got %q", out)
	}
}

func TestNewTextLoggerDebugSuppressed(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, false, false)
	l.Debug("should not appear")
	if buf.Len() != 0 {
		t.Fatalf("expected no output for debug at info level, got %q", buf.String())
	}
}

func TestNewTextLoggerVerboseShowsDebug(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, false, true)
	l.Debug("visible")
	if !strings.Contains(buf.String(), "visible") {
		t.Fatalf("expected debug output in verbose mode, got %q", buf.String())
	}
}

func TestNewJSONLogger(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, true, false)
	l.Warn("test-warn", "count", 42)
	out := buf.String()
	if !strings.Contains(out, `"msg":"test-warn"`) {
		t.Fatalf("expected JSON msg field, got %q", out)
	}
	if !strings.Contains(out, `"count":42`) {
		t.Fatalf("expected JSON count field, got %q", out)
	}
}

func TestNewNop(t *testing.T) {
	l := NewNop()
	l.Info("should not panic")
	l.Debug("should not panic")
	l.Warn("should not panic")
	l.Error("should not panic")
}

func TestWith(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, false, false)
	sub := l.With("component", "schema")
	sub.Info("copy done")
	out := buf.String()
	if !strings.Contains(out, "component=schema") {
		t.Fatalf("expected component=schema in output, got %q", out)
	}
}
