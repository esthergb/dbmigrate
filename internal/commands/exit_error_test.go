package commands

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/esthergb/dbmigrate/internal/config"
)

func TestWithExitCodeResolve(t *testing.T) {
	err := WithExitCode(ExitCodeDiff, errors.New("diff detected"))
	code, ok := ResolveExitCode(err)
	if !ok {
		t.Fatal("expected resolve to find exit code")
	}
	if code != ExitCodeDiff {
		t.Fatalf("unexpected exit code: got=%d want=%d", code, ExitCodeDiff)
	}
}

func TestRunReportReturnsDiffExitCodeOnAttentionRequired(t *testing.T) {
	tmp := t.TempDir()
	conflictPath := filepath.Join(tmp, "replication-conflict-report.json")
	raw := []byte(`{"version":1,"failure_type":"schema_drift","message":"apply failed"}`)
	if err := os.WriteFile(conflictPath, raw, 0o600); err != nil {
		t.Fatalf("write conflict report: %v", err)
	}

	err := runReport(context.Background(), config.RuntimeConfig{
		StateDir: tmp,
		JSON:     true,
	}, nil, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected report error")
	}
	code, ok := ResolveExitCode(err)
	if !ok {
		t.Fatal("expected explicit exit code on report attention error")
	}
	if code != ExitCodeDiff {
		t.Fatalf("unexpected exit code: got=%d want=%d", code, ExitCodeDiff)
	}
}

func TestRunVerifyReturnsVerifyFailedExitCodeOnInvalidConfig(t *testing.T) {
	err := runVerify(context.Background(), config.RuntimeConfig{}, nil, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected verify error")
	}

	code, ok := ResolveExitCode(err)
	if !ok {
		t.Fatal("expected explicit exit code on verify error")
	}
	if code != ExitCodeVerifyFailed {
		t.Fatalf("unexpected exit code: got=%d want=%d", code, ExitCodeVerifyFailed)
	}
}
