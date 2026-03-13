package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunHelp(t *testing.T) {
	var out bytes.Buffer
	code := Run(context.Background(), nil, &out, &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if out.Len() == 0 {
		t.Fatal("expected help output")
	}
}

func TestRunUnknownSubcommand(t *testing.T) {
	var out bytes.Buffer
	code := Run(context.Background(), []string{"unknown"}, &out, &out)
	if code != 1 {
		t.Fatalf("expected usage exit code 1, got %d", code)
	}
}

func TestRunPlanJSON(t *testing.T) {
	var out bytes.Buffer
	args := []string{"plan", "--source", "mysql://src", "--dest", "mysql://dst", "--json", "--dry-run"}
	code := Run(context.Background(), args, &out, &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if out.Len() == 0 {
		t.Fatal("expected command output")
	}
}

func TestRunWarnsOnPreferredTLSMode(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	args := []string{
		"plan",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--dry-run",
		"--tls-mode", "preferred",
	}
	code := Run(context.Background(), args, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "allows plaintext fallback") {
		t.Fatalf("expected preferred tls warning, got stderr=%q", stderr.String())
	}
}

func TestRunPlanInvalidDowngradeProfile(t *testing.T) {
	var out bytes.Buffer
	args := []string{"plan", "--source", "mysql://src", "--dest", "mysql://dst", "--downgrade-profile", "unsupported", "--dry-run"}
	code := Run(context.Background(), args, &out, &out)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d output=%s", code, out.String())
	}
}

func TestRunPlanInvalidDryRunMode(t *testing.T) {
	var out bytes.Buffer
	args := []string{"plan", "--source", "mysql://src", "--dest", "mysql://dst", "--dry-run-mode", "invalid", "--dry-run"}
	code := Run(context.Background(), args, &out, &out)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d output=%s", code, out.String())
	}
}

func TestRunPlanWithConfigFile(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "dbmigrate.yaml")
	cfg := []byte("source: mysql://cfg-src\ndest: mysql://cfg-dst\njson: true\ndry-run: true\n")
	if err := os.WriteFile(cfgPath, cfg, 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	var out bytes.Buffer
	args := []string{"plan", "--config", cfgPath}
	code := Run(context.Background(), args, &out, &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d output=%s", code, out.String())
	}
	if out.Len() == 0 {
		t.Fatal("expected command output")
	}
}

func TestRunPlanFlagOverridesConfig(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "dbmigrate.yaml")
	cfg := []byte("source: mysql://cfg-src\ndest: mysql://cfg-dst\n")
	if err := os.WriteFile(cfgPath, cfg, 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	var out bytes.Buffer
	args := []string{
		"plan",
		"--config", cfgPath,
		"--source", "mysql://flag-src",
		"--dest", "mysql://flag-dst",
		"--dry-run",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d output=%s", code, out.String())
	}
}

func TestRunMigrateDryRunSchemaOnly(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"migrate",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--schema-only",
		"--dry-run",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d output=%s", code, out.String())
	}
}

func TestRunMigrateDataOnlyNotImplemented(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"migrate",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--data-only",
		"--dry-run",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 0 {
		t.Fatalf("expected exit code 0 for dry-run data-only mode, got %d output=%s", code, out.String())
	}
}

func TestSplitGlobalAndCommandArgs(t *testing.T) {
	raw := []string{
		"--source", "mysql://src",
		"--dest=mysql://dst",
		"--downgrade-profile", "max-compat",
		"--dry-run-mode", "sandbox",
		"--operation-timeout", "15s",
		"--schema-only",
		"--force",
		"--json",
	}
	global, command, err := splitGlobalAndCommandArgs(raw)
	if err != nil {
		t.Fatalf("unexpected split error: %v", err)
	}

	if len(global) == 0 || len(command) == 0 {
		t.Fatalf("expected both global and command args, got global=%v command=%v", global, command)
	}
	if command[0] != "--schema-only" {
		t.Fatalf("expected schema-only in command args, got %v", command)
	}
	foundProfile := false
	for i := 0; i < len(global)-1; i++ {
		if global[i] == "--downgrade-profile" && global[i+1] == "max-compat" {
			foundProfile = true
			break
		}
	}
	if !foundProfile {
		t.Fatalf("expected downgrade-profile in global args, got %v", global)
	}
	foundDryRunMode := false
	for i := 0; i < len(global)-1; i++ {
		if global[i] == "--dry-run-mode" && global[i+1] == "sandbox" {
			foundDryRunMode = true
			break
		}
	}
	if !foundDryRunMode {
		t.Fatalf("expected dry-run-mode in global args, got %v", global)
	}
	foundTimeout := false
	for i := 0; i < len(global)-1; i++ {
		if global[i] == "--operation-timeout" && global[i+1] == "15s" {
			foundTimeout = true
			break
		}
	}
	if !foundTimeout {
		t.Fatalf("expected operation-timeout in global args, got %v", global)
	}
}

func TestSplitGlobalAndCommandArgsMissingValue(t *testing.T) {
	raw := []string{
		"--source",
		"--dest", "mysql://dst",
		"plan",
	}
	_, _, err := splitGlobalAndCommandArgs(raw)
	if err == nil {
		t.Fatal("expected split error for missing global flag value")
	}
}

func TestRunMigrateDryRunFullModeWithResume(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"migrate",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--dry-run",
		"--chunk-size", "500",
		"--resume",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d output=%s", code, out.String())
	}
}

func TestApplyOperationTimeoutDisabled(t *testing.T) {
	ctx := context.Background()
	got, cancel := applyOperationTimeout(ctx, 0)
	defer cancel()

	if got != ctx {
		t.Fatal("expected original context when timeout disabled")
	}
}

func TestApplyOperationTimeoutDeadline(t *testing.T) {
	ctx := context.Background()
	got, cancel := applyOperationTimeout(ctx, 25*time.Millisecond)
	defer cancel()

	deadline, ok := got.Deadline()
	if !ok {
		t.Fatal("expected deadline on timed context")
	}
	if time.Until(deadline) <= 0 {
		t.Fatal("expected future deadline")
	}
}

func TestRunVerifyDryRunSchema(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"verify",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--verify-level", "schema",
		"--dry-run",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d output=%s", code, out.String())
	}
}

func TestRunVerifyDryRunDataCount(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"verify",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--verify-level", "data",
		"--data-mode", "count",
		"--dry-run",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d output=%s", code, out.String())
	}
}

func TestRunVerifyInvalidLevel(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"verify",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--verify-level", "invalid",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 4 {
		t.Fatalf("expected exit code 4, got %d output=%s", code, out.String())
	}
}

func TestRunVerifyInvalidDataMode(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"verify",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--data-mode", "approx",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 4 {
		t.Fatalf("expected exit code 4, got %d output=%s", code, out.String())
	}
}

func TestRunVerifyDryRunDataHash(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"verify",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--verify-level", "data",
		"--data-mode", "hash",
		"--dry-run",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d output=%s", code, out.String())
	}
}

func TestRunVerifyDryRunDataSample(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"verify",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--verify-level", "data",
		"--data-mode", "sample",
		"--sample-size", "200",
		"--dry-run",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d output=%s", code, out.String())
	}
}

func TestRunVerifyDryRunDataFullHash(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"verify",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--verify-level", "data",
		"--data-mode", "full-hash",
		"--dry-run",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d output=%s", code, out.String())
	}
}

func TestRunVerifyInvalidSampleSize(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"verify",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--sample-size", "0",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 4 {
		t.Fatalf("expected exit code 4, got %d output=%s", code, out.String())
	}
}

func TestRunReplicateDryRun(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"replicate",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--dry-run",
		"--apply-ddl", "warn",
		"--resume",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d output=%s", code, out.String())
	}
}

func TestRunReplicateCaptureTriggersModeConnects(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"replicate",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--replication-mode", "capture-triggers",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 3 {
		t.Fatalf("expected exit code 3 (connect failure), got %d output=%s", code, out.String())
	}
}

func TestRunReplicateStartFromGTIDWithoutGTIDSetCLI(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"replicate",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--start-from", "gtid",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 3 {
		t.Fatalf("expected exit code 3 (parse error: --gtid-set required), got %d output=%s", code, out.String())
	}
}

func TestRunReplicateInvalidMaxEvents(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"replicate",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--max-events", "-1",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 3 {
		t.Fatalf("expected exit code 3, got %d output=%s", code, out.String())
	}
}

func TestRunReplicateInvalidMaxLagSeconds(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"replicate",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--max-lag-seconds", "-1",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 3 {
		t.Fatalf("expected exit code 3, got %d output=%s", code, out.String())
	}
}

func TestRunReplicateMaxLagSecondsDryRun(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"replicate",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--max-lag-seconds", "30",
		"--dry-run",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d output=%s", code, out.String())
	}
}

func TestRunReplicateInvalidIdempotentConflictPolicy(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"replicate",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--idempotent",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 3 {
		t.Fatalf("expected exit code 3, got %d output=%s", code, out.String())
	}
}

func TestRunReplicateEnableTriggerCDCConnects(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"replicate",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--enable-trigger-cdc",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 3 {
		t.Fatalf("expected exit code 3 (connect failure), got %d output=%s", code, out.String())
	}
}

func TestRunReplicateTeardownCDCConnects(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"replicate",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--teardown-cdc",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 3 {
		t.Fatalf("expected exit code 3 (connect failure), got %d output=%s", code, out.String())
	}
}

func TestRunReplicateInvalidApplyDDL(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"replicate",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--apply-ddl", "deny",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 3 {
		t.Fatalf("expected exit code 3, got %d output=%s", code, out.String())
	}
}

func TestRunReplicateInvalidConflictPolicy(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"replicate",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--conflict-policy", "merge",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 3 {
		t.Fatalf("expected exit code 3, got %d output=%s", code, out.String())
	}
}

func TestRunReportNoArtifacts(t *testing.T) {
	var out bytes.Buffer
	tmp := t.TempDir()
	args := []string{
		"report",
		"--state-dir", tmp,
		"--json",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d output=%s", code, out.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	if payload["status"] != "empty" {
		t.Fatalf("expected status empty, got %#v", payload["status"])
	}
}

func TestRunReportFailsByDefaultOnConflict(t *testing.T) {
	var out bytes.Buffer
	tmp := t.TempDir()
	conflictPath := filepath.Join(tmp, "replication-conflict-report.json")
	raw := []byte(`{"version":1,"failure_type":"schema_drift","message":"apply failed"}`)
	if err := os.WriteFile(conflictPath, raw, 0o600); err != nil {
		t.Fatalf("write conflict report: %v", err)
	}

	args := []string{
		"report",
		"--state-dir", tmp,
		"--json",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d output=%s", code, out.String())
	}
}

func TestRunReportConflictOverride(t *testing.T) {
	var out bytes.Buffer
	tmp := t.TempDir()
	conflictPath := filepath.Join(tmp, "replication-conflict-report.json")
	raw := []byte(`{"version":1,"failure_type":"schema_drift","message":"apply failed"}`)
	if err := os.WriteFile(conflictPath, raw, 0o600); err != nil {
		t.Fatalf("write conflict report: %v", err)
	}

	args := []string{
		"report",
		"--state-dir", tmp,
		"--json",
		"--fail-on-conflict=false",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d output=%s", code, out.String())
	}
}

func TestRunReportIgnoresStaleConflictWhenCheckpointAdvanced(t *testing.T) {
	var out bytes.Buffer
	tmp := t.TempDir()

	replicationPath := filepath.Join(tmp, "replication-checkpoint.json")
	replicationRaw := []byte(`{"version":1,"binlog_file":"mysql-bin.000300","binlog_pos":900,"apply_ddl":"warn"}`)
	if err := os.WriteFile(replicationPath, replicationRaw, 0o600); err != nil {
		t.Fatalf("write replication checkpoint: %v", err)
	}

	conflictPath := filepath.Join(tmp, "replication-conflict-report.json")
	conflictRaw := []byte(`{"version":1,"failure_type":"schema_drift","message":"apply failed","applied_end_file":"mysql-bin.000300","applied_end_pos":800}`)
	if err := os.WriteFile(conflictPath, conflictRaw, 0o600); err != nil {
		t.Fatalf("write conflict report: %v", err)
	}

	args := []string{
		"report",
		"--state-dir", tmp,
		"--json",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d output=%s", code, out.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("expected status ok, got %#v", payload["status"])
	}
}

func TestRunReportIgnoresStaleConflictByTimestampFallback(t *testing.T) {
	var out bytes.Buffer
	tmp := t.TempDir()

	replicationPath := filepath.Join(tmp, "replication-checkpoint.json")
	replicationRaw := []byte(`{"version":1,"binlog_file":"mysql-bin.000301","binlog_pos":900,"apply_ddl":"warn","updated_at":"2026-03-05T14:00:00Z"}`)
	if err := os.WriteFile(replicationPath, replicationRaw, 0o600); err != nil {
		t.Fatalf("write replication checkpoint: %v", err)
	}

	conflictPath := filepath.Join(tmp, "replication-conflict-report.json")
	conflictRaw := []byte(`{"version":1,"generated_at":"2026-03-05T13:30:00Z","failure_type":"schema_drift","message":"legacy conflict artifact"}`)
	if err := os.WriteFile(conflictPath, conflictRaw, 0o600); err != nil {
		t.Fatalf("write conflict report: %v", err)
	}

	args := []string{
		"report",
		"--state-dir", tmp,
		"--json",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d output=%s", code, out.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("expected status ok, got %#v", payload["status"])
	}
}
