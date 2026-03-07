package commands

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/esthergb/dbmigrate/internal/config"
)

func TestParseReplicateOptionsDefaults(t *testing.T) {
	opts, err := parseReplicateOptions(nil)
	if err != nil {
		t.Fatalf("expected parse success: %v", err)
	}
	if opts.ReplicationMode != "binlog" {
		t.Fatalf("expected default replication-mode binlog, got %q", opts.ReplicationMode)
	}
	if opts.StartFrom != "auto" {
		t.Fatalf("expected default start-from auto, got %q", opts.StartFrom)
	}
	if opts.MaxEvents != 0 {
		t.Fatalf("expected default max-events 0, got %d", opts.MaxEvents)
	}
	if opts.MaxLagSeconds != 0 {
		t.Fatalf("expected default max-lag-seconds 0, got %d", opts.MaxLagSeconds)
	}
	if opts.Idempotent {
		t.Fatal("expected default idempotent=false")
	}
	if opts.ApplyDDL != "warn" {
		t.Fatalf("expected default apply-ddl warn, got %q", opts.ApplyDDL)
	}
	if opts.ConflictPolicy != "fail" {
		t.Fatalf("expected default conflict-policy fail, got %q", opts.ConflictPolicy)
	}
	if opts.EnableTriggerCDC {
		t.Fatal("expected default enable-trigger-cdc=false")
	}
	if opts.TeardownCDC {
		t.Fatal("expected default teardown-cdc=false")
	}
	if !opts.Resume {
		t.Fatal("expected default resume=true")
	}
	if opts.StartPos != 4 {
		t.Fatalf("expected default start-pos 4, got %d", opts.StartPos)
	}
}

func TestParseReplicateOptionsExplicit(t *testing.T) {
	opts, err := parseReplicateOptions([]string{
		"--replication-mode=binlog",
		"--start-from=binlog-file:pos",
		"--max-events=250",
		"--max-lag-seconds=90",
		"--apply-ddl=ignore",
		"--conflict-policy=source-wins",
		"--enable-trigger-cdc",
		"--resume=false",
		"--start-file=mysql-bin.000010",
		"--start-pos=987",
	})
	if err != nil {
		t.Fatalf("expected parse success: %v", err)
	}
	if opts.ReplicationMode != "binlog" {
		t.Fatalf("expected replication-mode binlog, got %q", opts.ReplicationMode)
	}
	if opts.StartFrom != "binlog-file:pos" {
		t.Fatalf("expected start-from binlog-file:pos, got %q", opts.StartFrom)
	}
	if opts.MaxEvents != 250 {
		t.Fatalf("expected max-events 250, got %d", opts.MaxEvents)
	}
	if opts.MaxLagSeconds != 90 {
		t.Fatalf("expected max-lag-seconds 90, got %d", opts.MaxLagSeconds)
	}
	if opts.Idempotent {
		t.Fatal("expected idempotent=false")
	}
	if opts.ApplyDDL != "ignore" {
		t.Fatalf("expected apply-ddl ignore, got %q", opts.ApplyDDL)
	}
	if opts.ConflictPolicy != "source-wins" {
		t.Fatalf("expected conflict-policy source-wins, got %q", opts.ConflictPolicy)
	}
	if !opts.EnableTriggerCDC {
		t.Fatal("expected enable-trigger-cdc=true")
	}
	if opts.Resume {
		t.Fatal("expected resume=false")
	}
	if opts.StartFile != "mysql-bin.000010" {
		t.Fatalf("unexpected start-file %q", opts.StartFile)
	}
	if opts.StartPos != 987 {
		t.Fatalf("unexpected start-pos %d", opts.StartPos)
	}
}

func TestParseReplicateOptionsInvalidApplyDDL(t *testing.T) {
	_, err := parseReplicateOptions([]string{"--apply-ddl=deny"})
	if err == nil {
		t.Fatal("expected parse error for invalid apply-ddl")
	}
}

func TestParseReplicateOptionsInvalidReplicationMode(t *testing.T) {
	_, err := parseReplicateOptions([]string{"--replication-mode=stream"})
	if err == nil {
		t.Fatal("expected parse error for invalid replication-mode")
	}
}

func TestParseReplicateOptionsInvalidStartFrom(t *testing.T) {
	_, err := parseReplicateOptions([]string{"--start-from=snapshot"})
	if err == nil {
		t.Fatal("expected parse error for invalid start-from")
	}
}

func TestParseReplicateOptionsInvalidMaxEvents(t *testing.T) {
	_, err := parseReplicateOptions([]string{"--max-events=-1"})
	if err == nil {
		t.Fatal("expected parse error for invalid max-events")
	}
}

func TestParseReplicateOptionsInvalidMaxLagSeconds(t *testing.T) {
	_, err := parseReplicateOptions([]string{"--max-lag-seconds=-1"})
	if err == nil {
		t.Fatal("expected parse error for invalid max-lag-seconds")
	}
}

func TestParseReplicateOptionsIdempotentUnsupportedInV1(t *testing.T) {
	_, err := parseReplicateOptions([]string{"--idempotent"})
	if err == nil {
		t.Fatal("expected parse error for unsupported idempotent flag")
	}
	if !strings.Contains(err.Error(), "unsupported in v1") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseReplicateOptionsStartFromBinlogRequiresStartFile(t *testing.T) {
	_, err := parseReplicateOptions([]string{"--start-from=binlog-file:pos", "--resume=false"})
	if err == nil {
		t.Fatal("expected parse error for missing start-file with binlog-file:pos mode")
	}
}

func TestParseReplicateOptionsStartFromBinlogRequiresResumeFalse(t *testing.T) {
	_, err := parseReplicateOptions([]string{
		"--start-from=binlog-file:pos",
		"--start-file=mysql-bin.000010",
	})
	if err == nil {
		t.Fatal("expected parse error for resume=true with binlog-file:pos mode")
	}
}

func TestParseReplicateOptionsInvalidStartPos(t *testing.T) {
	_, err := parseReplicateOptions([]string{"--start-pos=3"})
	if err == nil {
		t.Fatal("expected parse error for invalid start-pos")
	}
}

func TestParseReplicateOptionsInvalidConflictPolicy(t *testing.T) {
	_, err := parseReplicateOptions([]string{"--conflict-policy=merge"})
	if err == nil {
		t.Fatal("expected parse error for invalid conflict-policy")
	}
}

func TestRunReplicateUnsupportedModeFailsFast(t *testing.T) {
	var out bytes.Buffer
	err := runReplicate(context.Background(), config.RuntimeConfig{
		Source: "mysql://src",
		Dest:   "mysql://dst",
	}, []string{"--replication-mode=capture-triggers"}, &out)
	if err == nil {
		t.Fatal("expected unsupported replication mode error")
	}
	code, ok := ResolveExitCode(err)
	if !ok || code != ExitCodeDiff {
		t.Fatalf("expected exit code %d, got code=%d ok=%v err=%v", ExitCodeDiff, code, ok, err)
	}
	if !strings.Contains(err.Error(), "not implemented yet") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunReplicateTriggerCDCFlagsFailFast(t *testing.T) {
	var out bytes.Buffer
	err := runReplicate(context.Background(), config.RuntimeConfig{
		Source: "mysql://src",
		Dest:   "mysql://dst",
	}, []string{"--enable-trigger-cdc"}, &out)
	if err == nil {
		t.Fatal("expected trigger cdc unsupported error")
	}
	code, ok := ResolveExitCode(err)
	if !ok || code != ExitCodeDiff {
		t.Fatalf("expected exit code %d, got code=%d ok=%v err=%v", ExitCodeDiff, code, ok, err)
	}
	if !strings.Contains(err.Error(), "trigger CDC mode is not implemented yet") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunReplicateStartFromGTIDFailsFast(t *testing.T) {
	var out bytes.Buffer
	err := runReplicate(context.Background(), config.RuntimeConfig{
		Source: "mysql://src",
		Dest:   "mysql://dst",
	}, []string{"--start-from=gtid"}, &out)
	if err == nil {
		t.Fatal("expected start-from gtid unsupported error")
	}
	code, ok := ResolveExitCode(err)
	if !ok || code != ExitCodeDiff {
		t.Fatalf("expected exit code %d, got code=%d ok=%v err=%v", ExitCodeDiff, code, ok, err)
	}
	if !strings.Contains(err.Error(), "start-from gtid is not implemented yet") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunReplicateMaxLagSecondsAllowedInDryRun(t *testing.T) {
	var out bytes.Buffer
	err := runReplicate(context.Background(), config.RuntimeConfig{
		Source: "mysql://src",
		Dest:   "mysql://dst",
		DryRun: true,
	}, []string{"--max-lag-seconds=30"}, &out)
	if err != nil {
		t.Fatalf("expected dry-run to succeed, got: %v", err)
	}
	if !strings.Contains(out.String(), "max_lag_seconds=30") {
		t.Fatalf("expected dry-run output to include max_lag_seconds, got %q", out.String())
	}
}
