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
	if opts.SourceServerID != 0 {
		t.Fatalf("expected default source-server-id 0, got %d", opts.SourceServerID)
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
		"--source-server-id=24001",
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
	if opts.SourceServerID != 24001 {
		t.Fatalf("expected source-server-id 24001, got %d", opts.SourceServerID)
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

func TestParseReplicateOptionsInvalidSourceServerID(t *testing.T) {
	_, err := parseReplicateOptions([]string{"--source-server-id=4294967296"})
	if err == nil {
		t.Fatal("expected parse error for invalid source-server-id")
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

func TestParseReplicateOptionsInvalidStartFile(t *testing.T) {
	_, err := parseReplicateOptions([]string{
		"--start-from=binlog-file:pos",
		"--resume=false",
		"--start-file=../../mysql-bin.000001",
	})
	if err == nil {
		t.Fatal("expected parse error for invalid start-file")
	}
}

func TestParseReplicateOptionsInvalidConflictPolicy(t *testing.T) {
	_, err := parseReplicateOptions([]string{"--conflict-policy=merge"})
	if err == nil {
		t.Fatal("expected parse error for invalid conflict-policy")
	}
}

func TestRunReplicateCaptureTriggersModeRequiresStateDir(t *testing.T) {
	var out bytes.Buffer
	err := runReplicate(context.Background(), config.RuntimeConfig{
		Source: "mysql://src",
		Dest:   "mysql://dst",
	}, []string{"--replication-mode=capture-triggers"}, &out)
	if err == nil {
		t.Fatal("expected error (state-dir or DB)")
	}
}

func TestRunReplicateEnableTriggerCDCRequiresCaptureTriggers(t *testing.T) {
	var out bytes.Buffer
	err := runReplicate(context.Background(), config.RuntimeConfig{
		Source: "mysql://src",
		Dest:   "mysql://dst",
	}, []string{"--enable-trigger-cdc"}, &out)
	if err == nil {
		t.Fatal("expected error (state-dir or DB required)")
	}
}

func TestRunReplicateStartFromGTIDRequiresGTIDSet(t *testing.T) {
	var out bytes.Buffer
	err := runReplicate(context.Background(), config.RuntimeConfig{
		Source: "mysql://src",
		Dest:   "mysql://dst",
	}, []string{"--start-from=gtid"}, &out)
	if err == nil {
		t.Fatal("expected parse error: --gtid-set required")
	}
	if !strings.Contains(err.Error(), "--gtid-set is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseReplicateOptionsGTIDSetRequiresStartFromGTID(t *testing.T) {
	_, err := parseReplicateOptions([]string{"--gtid-set=abc-123"})
	if err == nil {
		t.Fatal("expected error: --gtid-set without --start-from=gtid")
	}
	if !strings.Contains(err.Error(), "--gtid-set requires --start-from=gtid") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseReplicateOptionsGTIDSetValid(t *testing.T) {
	opts, err := parseReplicateOptions([]string{
		"--start-from=gtid",
		"--gtid-set=3E11FA47-71CA-11E1-9E33-C80AA9429562:1-23",
	})
	if err != nil {
		t.Fatalf("expected parse success: %v", err)
	}
	if opts.StartFrom != "gtid" {
		t.Fatalf("expected start-from gtid, got %q", opts.StartFrom)
	}
	if opts.GTIDSet != "3E11FA47-71CA-11E1-9E33-C80AA9429562:1-23" {
		t.Fatalf("unexpected gtid-set %q", opts.GTIDSet)
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
