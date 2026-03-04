package binlog

import (
	"errors"
	"fmt"
	"strings"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

type applyFailure struct {
	FailureType  string
	File         string
	Pos          uint32
	SQLErrorCode uint16
	Operation    string
	TableName    string
	Query        string
	ValueSample  []string
	Remediation  string
	Message      string
	Cause        error
	AppliedFile  string
	AppliedPos   uint32
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
		ValueSample: buildValueSample(event.KeyArgs),
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

func buildValueSample(values []any) []string {
	if len(values) == 0 {
		return nil
	}

	limit := len(values)
	if limit > 6 {
		limit = 6
	}

	sample := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		sample = append(sample, fmt.Sprintf("v%d=%s", i+1, sampleValue(values[i])))
	}
	if len(values) > limit {
		sample = append(sample, fmt.Sprintf("... +%d more", len(values)-limit))
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
