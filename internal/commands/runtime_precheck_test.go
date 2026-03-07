package commands

import (
	"strings"
	"testing"

	"github.com/esthergb/dbmigrate/internal/compat"
)

func TestBuildReplicationReadinessFindings(t *testing.T) {
	report := replicationReadinessPrecheckReport{
		SourceLogBinKnown:           true,
		SourceLogBin:                false,
		SourceBinlogFormat:          "STATEMENT",
		SourceBinlogRowImage:        "MINIMAL",
		SourceMasterStatusAvailable: true,
		SourceMasterStatusFile:      "mysql-bin.000123",
		SourceMasterStatusPos:       456,
	}
	findings := buildReplicationReadinessFindings(
		report,
		compat.ParseInstance("8.4.8 MySQL Community Server - GPL"),
		compat.ParseInstance("11.4.8-MariaDB"),
	)
	codes := make([]string, 0, len(findings))
	for _, finding := range findings {
		codes = append(codes, finding.Code)
	}
	joined := strings.Join(codes, ",")
	for _, code := range []string{
		"replication_readiness_source_log_bin_disabled",
		"replication_readiness_non_row_binlog_format",
		"replication_readiness_non_full_row_image",
		"replication_readiness_binary_log_status_recorded",
		"replication_readiness_cross_engine_lane",
	} {
		if !strings.Contains(joined, code) {
			t.Fatalf("expected code %s in %#v", code, codes)
		}
	}
}

func TestNormalizeTimeZoneSetting(t *testing.T) {
	if got := normalizeTimeZoneSetting("CET", "SYSTEM"); got != "CET" {
		t.Fatalf("normalizeTimeZoneSetting(system)= %q", got)
	}
	if got := normalizeTimeZoneSetting("CET", "+00:00"); got != "+00:00" {
		t.Fatalf("normalizeTimeZoneSetting(global)= %q", got)
	}
}

func TestBuildTimezonePortabilityFindings(t *testing.T) {
	report := timezonePortabilityPrecheckReport{
		SourceSystemTimeZone:    "CET",
		SourceGlobalTimeZone:    "SYSTEM",
		SourceSessionTimeZone:   "SYSTEM",
		DestSystemTimeZone:      "UTC",
		DestGlobalTimeZone:      "+00:00",
		DestSessionTimeZone:     "+00:00",
		TemporalTableCount:      2,
		MixedTemporalTableCount: 1,
		Tables: []temporalTableIssue{
			{Database: "app", Table: "audit_log", TimestampCount: 1, DatetimeCount: 1, Proposal: "review mixed semantics"},
			{Database: "app", Table: "events", TimestampCount: 2, DatetimeCount: 0, Proposal: "review timestamp rendering"},
		},
	}
	findings := buildTimezonePortabilityFindings(report)
	codes := make([]string, 0, len(findings))
	for _, finding := range findings {
		codes = append(codes, finding.Code)
	}
	joined := strings.Join(codes, ",")
	for _, code := range []string{
		"timezone_inventory_recorded",
		"timezone_environment_drift_detected",
		"mixed_timestamp_datetime_tables_detected",
		"mixed_timestamp_datetime_table",
		"timestamp_table_timezone_review",
	} {
		if !strings.Contains(joined, code) {
			t.Fatalf("expected code %s in %#v", code, codes)
		}
	}
}
