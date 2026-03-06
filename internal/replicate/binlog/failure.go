package binlog

import (
	"errors"
	"fmt"
	"strings"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

type applyFailure struct {
	FailureType   string
	File          string
	Pos           uint32
	SQLErrorCode  uint16
	Operation     string
	TableName     string
	Query         string
	ValueSample   []string
	OldRowSample  []string
	NewRowSample  []string
	RowDiffSample []string
	Remediation   string
	Message       string
	Cause         error
	AppliedFile   string
	AppliedPos    uint32
}

func (f *applyFailure) Error() string {
	if f == nil {
		return "replication apply failure"
	}
	base := f.Message
	if base == "" {
		base = "replication apply failure"
	}
	if f.Cause != nil {
		return fmt.Sprintf("%s: %v", base, f.Cause)
	}
	return base
}

func classifyApplySQLError(cause error, event applyEvent, file string, pos uint32, appliedFile string, appliedPos uint32) *applyFailure {
	failure := &applyFailure{
		FailureType: "apply_sql_error",
		File:        file,
		Pos:         pos,
		Operation:   event.Operation,
		TableName:   event.TableName,
		Query:       event.Query,
		ValueSample: buildValueSample(event.KeyColumns, event.KeyArgs),
		OldRowSample: buildValueSample(
			event.RowColumns,
			event.OldRowArgs,
		),
		NewRowSample: buildValueSample(
			event.RowColumns,
			event.NewRowArgs,
		),
		RowDiffSample: buildRowDiffSample(
			event.RowColumns,
			event.OldRowArgs,
			event.NewRowArgs,
		),
		Message:     fmt.Sprintf("apply event at %s:%d failed", file, pos),
		Cause:       cause,
		Remediation: "review table schema and conflicting destination rows, then rerun replicate from checkpoint",
		AppliedFile: appliedFile,
		AppliedPos:  appliedPos,
	}

	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(cause, &mysqlErr) && mysqlErr != nil {
		failure.SQLErrorCode = mysqlErr.Number
		switch mysqlErr.Number {
		case 1062:
			failure.FailureType = "conflict_duplicate_key"
			failure.Remediation = "resolve duplicate key conflict or rerun with --conflict-policy=source-wins or --conflict-policy=dest-wins"
		case 1451, 1452:
			failure.FailureType = "conflict_foreign_key"
			failure.Remediation = "verify parent/child row ordering and schema constraints, then rerun replicate"
		case 1054, 1136, 1146, 1051:
			failure.FailureType = "schema_drift"
			failure.Remediation = "run migrate --schema-only to align schema, then rerun replicate"
		case 1044, 1045, 1142:
			failure.FailureType = "permission_denied"
			failure.Remediation = "grant required destination privileges for replication apply and rerun"
		case 1366, 1406, 1265:
			failure.FailureType = "data_conversion_error"
			failure.Remediation = "inspect source/destination column types and sql_mode, then rerun after remediation"
		case 1213, 1205:
			failure.FailureType = "retryable_transaction_error"
			failure.Remediation = "retry replicate; if recurring, reduce batch size or lock contention on destination"
			if looksLikeMetadataLockFailure(mysqlErr.Message, event) {
				failure.FailureType = "metadata_lock_timeout"
				failure.Remediation = "inspect SHOW FULL PROCESSLIST and performance_schema.metadata_locks (or MariaDB metadata_lock_info), identify the blocking session, abort the waiting DDL if blast radius is growing, then rerun during a drained window"
			}
		}
		return failure
	}

	messageUpper := strings.ToUpper(cause.Error())
	if strings.Contains(messageUpper, "UNKNOWN COLUMN") || strings.Contains(messageUpper, "DOESN'T EXIST") {
		failure.FailureType = "schema_drift"
		failure.Remediation = "run migrate --schema-only to align schema, then rerun replicate"
	}

	return failure
}

func looksLikeMetadataLockFailure(message string, event applyEvent) bool {
	if !strings.EqualFold(strings.TrimSpace(event.Operation), "ddl") {
		return false
	}
	upper := strings.ToUpper(message)
	return strings.Contains(upper, "METADATA LOCK") || strings.Contains(upper, "TABLE METADATA LOCK")
}

func buildValueSample(columns []string, values []any) []string {
	if len(values) == 0 {
		return nil
	}

	limit := len(values)
	if limit > 6 {
		limit = 6
	}

	sample := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		label := fmt.Sprintf("v%d", i+1)
		if i < len(columns) && strings.TrimSpace(columns[i]) != "" {
			label = strings.TrimSpace(columns[i])
		}
		sample = append(sample, fmt.Sprintf("%s=%s", label, sampleValue(values[i])))
	}
	if len(values) > limit {
		sample = append(sample, fmt.Sprintf("... +%d more", len(values)-limit))
	}
	return sample
}

func buildRowDiffSample(columns []string, oldValues []any, newValues []any) []string {
	if len(oldValues) == 0 || len(newValues) == 0 {
		return nil
	}

	limit := len(oldValues)
	if len(newValues) < limit {
		limit = len(newValues)
	}
	if limit == 0 {
		return nil
	}

	const maxChanges = 6
	sample := make([]string, 0, maxChanges+1)
	diffCount := 0
	for i := 0; i < limit; i++ {
		oldText := sampleValue(oldValues[i])
		newText := sampleValue(newValues[i])
		if oldText == newText {
			continue
		}
		diffCount++
		if len(sample) >= maxChanges {
			continue
		}
		label := fmt.Sprintf("v%d", i+1)
		if i < len(columns) && strings.TrimSpace(columns[i]) != "" {
			label = strings.TrimSpace(columns[i])
		}
		sample = append(sample, fmt.Sprintf("%s:%s->%s", label, oldText, newText))
	}

	if diffCount == 0 {
		return nil
	}
	if diffCount > maxChanges {
		sample = append(sample, fmt.Sprintf("... +%d more changes", diffCount-maxChanges))
	}
	return sample
}

func sampleValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return "NULL"
	case []byte:
		return truncateForSample(string(typed))
	case string:
		return truncateForSample(typed)
	default:
		return truncateForSample(fmt.Sprint(value))
	}
}

func truncateForSample(value string) string {
	normalized := strings.ReplaceAll(value, "\n", " ")
	normalized = strings.ReplaceAll(normalized, "\r", " ")
	if len(normalized) <= 80 {
		return normalized
	}
	return normalized[:80] + "..."
}
