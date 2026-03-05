package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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

func TestRunPlanInvalidDowngradeProfile(t *testing.T) {
	var out bytes.Buffer
	args := []string{"plan", "--source", "mysql://src", "--dest", "mysql://dst", "--downgrade-profile", "unsupported", "--dry-run"}
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
		"--schema-only",
		"--force",
		"--json",
	}
	global, command := splitGlobalAndCommandArgs(raw)

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

func TestRunReplicateUnsupportedMode(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"replicate",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--replication-mode", "capture-triggers",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 3 {
		t.Fatalf("expected exit code 3, got %d output=%s", code, out.String())
	}
}

func TestRunReplicateUnsupportedStartFromGTID(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"replicate",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--start-from", "gtid",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 3 {
		t.Fatalf("expected exit code 3, got %d output=%s", code, out.String())
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

func TestRunReplicateEnableTriggerCDCUnsupported(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"replicate",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--enable-trigger-cdc",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 3 {
		t.Fatalf("expected exit code 3, got %d output=%s", code, out.String())
	}
}

func TestRunReplicateTeardownCDCUnsupported(t *testing.T) {
	var out bytes.Buffer
	args := []string{
		"replicate",
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--teardown-cdc",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 3 {
		t.Fatalf("expected exit code 3, got %d output=%s", code, out.String())
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
