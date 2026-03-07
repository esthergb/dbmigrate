package commands

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/esthergb/dbmigrate/internal/compat"
)

type replicationReadinessPrecheckReport struct {
	Name                        string           `json:"name"`
	SourceVersion               string           `json:"source_version"`
	DestVersion                 string           `json:"dest_version"`
	SourceLogBinKnown           bool             `json:"source_log_bin_known"`
	SourceLogBin                bool             `json:"source_log_bin,omitempty"`
	SourceBinlogFormat          string           `json:"source_binlog_format,omitempty"`
	SourceBinlogRowImage        string           `json:"source_binlog_row_image,omitempty"`
	SourceMasterStatusAvailable bool             `json:"source_master_status_available"`
	SourceMasterStatusFile      string           `json:"source_master_status_file,omitempty"`
	SourceMasterStatusPos       uint32           `json:"source_master_status_pos,omitempty"`
	Findings                    []compat.Finding `json:"findings,omitempty"`
}

func runReplicationReadinessPrecheck(
	ctx context.Context,
	source *sql.DB,
	dest *sql.DB,
	sourceInstance compat.Instance,
	destInstance compat.Instance,
) (replicationReadinessPrecheckReport, error) {
	report := replicationReadinessPrecheckReport{
		Name:          "replication-readiness",
		SourceVersion: sourceInstance.RawVersion,
		DestVersion:   destInstance.RawVersion,
	}
	if source == nil || dest == nil {
		return report, fmt.Errorf("source and destination connections are required")
	}

	var logBinRaw any
	var formatRaw sql.NullString
	var rowImageRaw sql.NullString
	if err := source.QueryRowContext(ctx, "SELECT @@GLOBAL.log_bin, @@GLOBAL.binlog_format, @@GLOBAL.binlog_row_image").Scan(&logBinRaw, &formatRaw, &rowImageRaw); err != nil {
		report.Findings = append(report.Findings, compat.Finding{
			Code:     "replication_readiness_inventory_unavailable",
			Severity: "warn",
			Message:  fmt.Sprintf("Unable to read source replication readiness variables: %v.", err),
			Proposal: "Validate source log_bin, binlog_format, and binlog_row_image manually before relying on incremental continuity.",
		})
	} else {
		value, parseErr := parseLogBinEnabled(logBinRaw)
		if parseErr != nil {
			report.Findings = append(report.Findings, compat.Finding{
				Code:     "replication_readiness_log_bin_parse_failed",
				Severity: "warn",
				Message:  fmt.Sprintf("Unable to interpret source @@GLOBAL.log_bin value: %v.", parseErr),
				Proposal: "Validate source binary logging manually before using incremental continuity.",
			})
		} else {
			report.SourceLogBinKnown = true
			report.SourceLogBin = value
		}
		report.SourceBinlogFormat = strings.TrimSpace(formatRaw.String)
		report.SourceBinlogRowImage = strings.TrimSpace(rowImageRaw.String)
	}

	file, pos, statusErr := queryBinaryLogStatus(ctx, source)
	if statusErr == nil {
		report.SourceMasterStatusAvailable = true
		report.SourceMasterStatusFile = file
		report.SourceMasterStatusPos = pos
	} else {
		report.Findings = append(report.Findings, compat.Finding{
			Code:     "replication_readiness_master_status_unavailable",
			Severity: "warn",
			Message:  fmt.Sprintf("Unable to read source binary log status for continuity handoff: %v.", statusErr),
			Proposal: "Grant access to SHOW MASTER STATUS / SHOW BINARY LOG STATUS or collect file/position evidence with a privileged account before incremental cutover.",
		})
	}

	report.Findings = append(report.Findings, buildReplicationReadinessFindings(report, sourceInstance, destInstance)...)
	return report, nil
}

func buildReplicationReadinessFindings(report replicationReadinessPrecheckReport, sourceInstance compat.Instance, destInstance compat.Instance) []compat.Finding {
	findings := make([]compat.Finding, 0, 8)
	if report.SourceLogBinKnown && !report.SourceLogBin {
		findings = append(findings, compat.Finding{
			Code:     "replication_readiness_source_log_bin_disabled",
			Severity: "warn",
			Message:  "Source log_bin is disabled; incremental continuity is unavailable until binary logging is enabled.",
			Proposal: "Enable source binary logging, rotate to fresh binlogs, and capture a new baseline watermark before using replicate.",
		})
	}
	if report.SourceBinlogFormat != "" && !strings.EqualFold(report.SourceBinlogFormat, "ROW") {
		findings = append(findings, compat.Finding{
			Code:     "replication_readiness_non_row_binlog_format",
			Severity: "warn",
			Message:  fmt.Sprintf("Source binlog_format=%q; v1 incremental replay assumes ROW logging for deterministic behavior.", report.SourceBinlogFormat),
			Proposal: "Switch source binlog_format to ROW before collecting continuity evidence for replicate.",
		})
	}
	if report.SourceBinlogRowImage != "" && !strings.EqualFold(report.SourceBinlogRowImage, "FULL") {
		findings = append(findings, compat.Finding{
			Code:     "replication_readiness_non_full_row_image",
			Severity: "warn",
			Message:  fmt.Sprintf("Source binlog_row_image=%q; v1 row replay requires FULL row images.", report.SourceBinlogRowImage),
			Proposal: "Switch source binlog_row_image to FULL before collecting continuity evidence for replicate.",
		})
	}
	if report.SourceMasterStatusAvailable {
		findings = append(findings, compat.Finding{
			Code:     "replication_readiness_binary_log_status_recorded",
			Severity: "info",
			Message:  fmt.Sprintf("Recorded source binary log handoff position %s:%d for continuity planning.", report.SourceMasterStatusFile, report.SourceMasterStatusPos),
			Proposal: "Keep file/position evidence with the plan artifact so baseline and incremental cutover share the same handoff reference.",
		})
	}
	if sourceInstance.Engine != destInstance.Engine {
		findings = append(findings, compat.Finding{
			Code:     "replication_readiness_cross_engine_lane",
			Severity: "warn",
			Message:  fmt.Sprintf("Replication readiness inventory applies to a cross-engine lane (%s -> %s); keep file/position evidence and rehearse the exact boundary settings.", sourceInstance.Engine, destInstance.Engine),
			Proposal: "Do not treat a green baseline alone as proof that incremental cross-engine continuity is safe.",
		})
	}
	if len(findings) == 0 {
		findings = append(findings, compat.Finding{
			Code:     "replication_readiness_inventory_clean",
			Severity: "info",
			Message:  "Replication-readiness inventory did not reveal obvious source binlog configuration blockers.",
			Proposal: "Keep the readiness inventory artifact with the plan output before approving incremental cutover.",
		})
	}
	return findings
}

func parseLogBinEnabled(value any) (bool, error) {
	switch typed := value.(type) {
	case int64:
		return typed != 0, nil
	case uint64:
		return typed != 0, nil
	case int:
		return typed != 0, nil
	case uint:
		return typed != 0, nil
	case []byte:
		return parseLogBinEnabled(string(typed))
	case string:
		normalized := strings.ToUpper(strings.TrimSpace(typed))
		switch normalized {
		case "1", "ON", "TRUE":
			return true, nil
		case "0", "OFF", "FALSE":
			return false, nil
		default:
			return false, fmt.Errorf("unsupported log_bin value %q", typed)
		}
	default:
		return false, fmt.Errorf("unsupported log_bin type %T", value)
	}
}

func queryBinaryLogStatus(ctx context.Context, db *sql.DB) (string, uint32, error) {
	queries := []string{"SHOW MASTER STATUS", "SHOW BINARY LOG STATUS"}
	for _, query := range queries {
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			continue
		}
		file, pos, readErr := scanBinaryLogStatusRows(rows)
		if readErr == nil {
			return file, pos, nil
		}
	}
	return "", 0, fmt.Errorf("binary log status unavailable")
}

func scanBinaryLogStatusRows(rows *sql.Rows) (string, uint32, error) {
	defer func() {
		_ = rows.Close()
	}()
	columns, err := rows.Columns()
	if err != nil {
		return "", 0, err
	}
	if len(columns) < 2 {
		return "", 0, fmt.Errorf("unexpected binary log status result format")
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", 0, err
		}
		return "", 0, sql.ErrNoRows
	}
	values := make([]sql.RawBytes, len(columns))
	scanArgs := make([]any, len(columns))
	for i := range values {
		scanArgs[i] = &values[i]
	}
	if err := rows.Scan(scanArgs...); err != nil {
		return "", 0, err
	}
	file := strings.TrimSpace(string(values[0]))
	if file == "" {
		return "", 0, fmt.Errorf("empty binary log file")
	}
	posValue := strings.TrimSpace(string(values[1]))
	pos, err := strconv.ParseUint(posValue, 10, 32)
	if err != nil {
		return "", 0, err
	}
	return file, uint32(pos), nil
}
