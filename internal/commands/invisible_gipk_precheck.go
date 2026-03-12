package commands

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/esthergb/dbmigrate/internal/compat"
	"github.com/esthergb/dbmigrate/internal/config"
	"github.com/esthergb/dbmigrate/internal/schema"
	"github.com/esthergb/dbmigrate/internal/state"
	"github.com/esthergb/dbmigrate/internal/version"
)

type invisibleColumnIssue struct {
	Database string `json:"database"`
	Table    string `json:"table"`
	Column   string `json:"column"`
	Extra    string `json:"extra"`
	Proposal string `json:"proposal"`
}

type invisibleIndexIssue struct {
	Database string `json:"database"`
	Table    string `json:"table"`
	Index    string `json:"index"`
	Proposal string `json:"proposal"`
}

type generatedInvisiblePrimaryKeyIssue struct {
	Database string `json:"database"`
	Table    string `json:"table"`
	Column   string `json:"column"`
	Proposal string `json:"proposal"`
}

type invisibleGIPKPrecheckReport struct {
	Name                            string                              `json:"name"`
	Incompatible                    bool                                `json:"incompatible"`
	SourceVersion                   string                              `json:"source_version"`
	DestVersion                     string                              `json:"dest_version"`
	SourceShowGIPKVariablePresent   bool                                `json:"source_show_gipk_variable_present"`
	SourceShowGIPKEnabled           bool                                `json:"source_show_gipk_enabled"`
	SourceGenerateGIPKVariableKnown bool                                `json:"source_generate_gipk_variable_known"`
	SourceGenerateGIPKEnabled       bool                                `json:"source_generate_gipk_enabled"`
	InvisibleColumnCount            int                                 `json:"invisible_column_count"`
	InvisibleIndexCount             int                                 `json:"invisible_index_count"`
	GIPKTableCount                  int                                 `json:"gipk_table_count"`
	InvisibleColumns                []invisibleColumnIssue              `json:"invisible_columns,omitempty"`
	InvisibleIndexes                []invisibleIndexIssue               `json:"invisible_indexes,omitempty"`
	GIPKTables                      []generatedInvisiblePrimaryKeyIssue `json:"gipk_tables,omitempty"`
	Findings                        []compat.Finding                    `json:"findings,omitempty"`
}

type invisibleGIPKPrecheckResult struct {
	Command   string                      `json:"command"`
	Status    string                      `json:"status"`
	Precheck  invisibleGIPKPrecheckReport `json:"precheck"`
	Timestamp time.Time                   `json:"timestamp"`
	Version   string                      `json:"version"`
}

func runInvisibleGIPKPrecheck(
	ctx context.Context,
	source *sql.DB,
	dest *sql.DB,
	stateDir string,
	includeDatabases []string,
	excludeDatabases []string,
) (invisibleGIPKPrecheckReport, error) {
	report := invisibleGIPKPrecheckReport{
		Name: "invisible-gipk",
	}

	sourceVersion, err := queryServerVersion(ctx, source)
	if err != nil {
		return report, fmt.Errorf("detect source version: %w", err)
	}
	destVersion, err := queryServerVersion(ctx, dest)
	if err != nil {
		return report, fmt.Errorf("detect destination version: %w", err)
	}
	report.SourceVersion = sourceVersion
	report.DestVersion = destVersion

	sourceInstance := compat.ParseInstance(sourceVersion)
	destInstance := compat.ParseInstance(destVersion)

	if sourceInstance.Engine == compat.EngineMySQL {
		value, present, err := queryOptionalBooleanVariable(ctx, source, "@@show_gipk_in_create_table_and_information_schema")
		report.SourceShowGIPKVariablePresent = present
		report.SourceShowGIPKEnabled = value
		if err != nil {
			report.Findings = append(report.Findings, compat.Finding{
				Code:     "source_show_gipk_inventory_unavailable",
				Severity: "warn",
				Message:  fmt.Sprintf("Unable to read source @@show_gipk_in_create_table_and_information_schema: %v.", err),
				Proposal: "Keep GIPK inventory on and visible during plan/migrate runs so generated invisible primary keys are not hidden from precheck output.",
			})
		}

		value, present, err = queryOptionalBooleanVariable(ctx, source, "@@sql_generate_invisible_primary_key")
		report.SourceGenerateGIPKVariableKnown = present
		report.SourceGenerateGIPKEnabled = value
		if err != nil {
			report.Findings = append(report.Findings, compat.Finding{
				Code:     "source_generate_gipk_variable_unavailable",
				Severity: "warn",
				Message:  fmt.Sprintf("Unable to read source @@sql_generate_invisible_primary_key: %v.", err),
				Proposal: "Do not assume GIPK generation policy is disabled just because the variable is not visible to this session.",
			})
		}
	}

	allDatabases, err := listDatabases(ctx, source)
	if err != nil {
		return report, fmt.Errorf("list source databases: %w", err)
	}
	selectedDatabases := schema.SelectDatabases(allDatabases, includeDatabases, excludeDatabases)
	if len(selectedDatabases) == 0 {
		report.Findings = append(report.Findings, compat.Finding{
			Code:     "invisible_gipk_no_selected_databases",
			Severity: "info",
			Message:  "No source databases selected for invisible-column or GIPK inventory.",
			Proposal: "Set --databases or verify include/exclude filters if downgrade evidence should be scoped to a partial migration set.",
		})
		return report, nil
	}

	report.InvisibleColumns, err = queryInvisibleColumnIssues(ctx, source, selectedDatabases)
	if err != nil {
		return report, err
	}
	report.InvisibleColumnCount = len(report.InvisibleColumns)

	if sourceInstance.Engine == compat.EngineMySQL {
		report.InvisibleIndexes, err = queryInvisibleIndexIssues(ctx, source, selectedDatabases)
		if err != nil {
			return report, err
		}
		report.InvisibleIndexCount = len(report.InvisibleIndexes)

		report.GIPKTables, err = queryGeneratedInvisiblePrimaryKeyIssues(ctx, source, selectedDatabases)
		if err != nil {
			return report, err
		}
		report.GIPKTableCount = len(report.GIPKTables)
	}

	if report.SourceShowGIPKVariablePresent && !report.SourceShowGIPKEnabled {
		report.Findings = append(report.Findings, compat.Finding{
			Code:     "source_show_gipk_disabled",
			Severity: "warn",
			Message:  "Source @@show_gipk_in_create_table_and_information_schema is disabled; generated invisible primary key inventory may be incomplete.",
			Proposal: "Enable source show_gipk_in_create_table_and_information_schema while collecting evidence so hidden primary keys are not silently omitted from plan output.",
		})
	}

	report.Findings = append(report.Findings, buildInvisibleGIPKFindings(sourceInstance, destInstance, report)...)
	report.Incompatible = invisibleGIPKIncompatible(sourceInstance, destInstance, report)
	if err := persistInvisibleGIPKPrecheckArtifact(stateDir, report); err != nil {
		return report, err
	}
	return report, nil
}

func invisibleGIPKPrecheckArtifactPath(stateDir string) string {
	baseDir := strings.TrimSpace(stateDir)
	if baseDir == "" {
		baseDir = "./state"
	}
	return filepath.Join(baseDir, "invisible-gipk-precheck.json")
}

func persistInvisibleGIPKPrecheckArtifact(stateDir string, report invisibleGIPKPrecheckReport) error {
	path := invisibleGIPKPrecheckArtifactPath(stateDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir state-dir for invisible/gipk precheck artifact: %w", err)
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal invisible/gipk precheck artifact: %w", err)
	}
	if err := state.WritePrivateFileAtomic(path, append(raw, '\n')); err != nil {
		return fmt.Errorf("write invisible/gipk precheck artifact: %w", err)
	}
	return nil
}

func loadInvisibleGIPKPrecheckArtifact(stateDir string) (invisibleGIPKPrecheckReport, error) {
	path := invisibleGIPKPrecheckArtifactPath(stateDir)
	raw, err := os.ReadFile(path)
	if err != nil {
		return invisibleGIPKPrecheckReport{}, fmt.Errorf("read invisible/gipk precheck artifact: %w", err)
	}
	var report invisibleGIPKPrecheckReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return invisibleGIPKPrecheckReport{}, fmt.Errorf("parse invisible/gipk precheck artifact: %w", err)
	}
	return report, nil
}

func queryInvisibleColumnIssues(ctx context.Context, source *sql.DB, databases []string) ([]invisibleColumnIssue, error) {
	placeholders, args := sqlPlaceholders(databases)
	query := fmt.Sprintf(`
		SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, EXTRA
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA IN (%s)
		  AND UPPER(COALESCE(EXTRA, '')) LIKE '%%INVISIBLE%%'
		  AND NOT (
		    COLUMN_KEY = 'PRI'
		    AND UPPER(COALESCE(EXTRA, '')) LIKE '%%AUTO_INCREMENT%%'
		    AND COLUMN_NAME = 'my_row_id'
		  )
		ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION
	`, placeholders)

	rows, err := source.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query source invisible columns: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]invisibleColumnIssue, 0, 8)
	for rows.Next() {
		var issue invisibleColumnIssue
		if err := rows.Scan(&issue.Database, &issue.Table, &issue.Column, &issue.Extra); err != nil {
			return nil, err
		}
		issue.Extra = strings.TrimSpace(issue.Extra)
		issue.Proposal = fmt.Sprintf(
			"Materialize %s.%s column %s as visible before downgrade or cross-engine migration if the destination cannot preserve invisible-column semantics.",
			issue.Database,
			issue.Table,
			issue.Column,
		)
		out = append(out, issue)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func queryInvisibleIndexIssues(ctx context.Context, source *sql.DB, databases []string) ([]invisibleIndexIssue, error) {
	placeholders, args := sqlPlaceholders(databases)
	query := fmt.Sprintf(`
		SELECT TABLE_SCHEMA, TABLE_NAME, INDEX_NAME
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA IN (%s)
		  AND INDEX_NAME <> 'PRIMARY'
		  AND UPPER(COALESCE(IS_VISIBLE, 'YES')) = 'NO'
		GROUP BY TABLE_SCHEMA, TABLE_NAME, INDEX_NAME
		ORDER BY TABLE_SCHEMA, TABLE_NAME, INDEX_NAME
	`, placeholders)

	rows, err := source.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query source invisible indexes: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]invisibleIndexIssue, 0, 8)
	for rows.Next() {
		var issue invisibleIndexIssue
		if err := rows.Scan(&issue.Database, &issue.Table, &issue.Index); err != nil {
			return nil, err
		}
		issue.Proposal = fmt.Sprintf(
			"Decide whether %s.%s index %s must stay hidden. Destinations that do not preserve invisible indexes will expose it as a normal index.",
			issue.Database,
			issue.Table,
			issue.Index,
		)
		out = append(out, issue)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func queryGeneratedInvisiblePrimaryKeyIssues(ctx context.Context, source *sql.DB, databases []string) ([]generatedInvisiblePrimaryKeyIssue, error) {
	placeholders, args := sqlPlaceholders(databases)
	query := fmt.Sprintf(`
		SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA IN (%s)
		  AND COLUMN_KEY = 'PRI'
		  AND UPPER(COALESCE(EXTRA, '')) LIKE '%%INVISIBLE%%'
		  AND UPPER(COALESCE(EXTRA, '')) LIKE '%%AUTO_INCREMENT%%'
		ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION
	`, placeholders)

	rows, err := source.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query source generated invisible primary keys: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]generatedInvisiblePrimaryKeyIssue, 0, 8)
	for rows.Next() {
		var issue generatedInvisiblePrimaryKeyIssue
		if err := rows.Scan(&issue.Database, &issue.Table, &issue.Column); err != nil {
			return nil, err
		}
		issue.Proposal = fmt.Sprintf(
			"Keep dump mode explicit for %s.%s: including GIPK preserves the hidden key on supported MySQL targets, while --skip-generated-invisible-primary-key drops it from logical dumps entirely.",
			issue.Database,
			issue.Table,
		)
		out = append(out, issue)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func buildInvisibleGIPKFindings(source compat.Instance, dest compat.Instance, report invisibleGIPKPrecheckReport) []compat.Finding {
	if report.InvisibleColumnCount == 0 && report.InvisibleIndexCount == 0 && report.GIPKTableCount == 0 {
		return []compat.Finding{{
			Code:     "invisible_gipk_inventory_clean",
			Severity: "info",
			Message:  "Selected source databases do not use invisible columns, invisible indexes, or generated invisible primary keys.",
			Proposal: "No Phase 62 downgrade evidence is required for this scope. Keep the rehearsal script for future MySQL-source paths that introduce hidden schema features.",
		}}
	}

	findings := make([]compat.Finding, 0, 1+len(report.InvisibleColumns)+len(report.InvisibleIndexes)+len(report.GIPKTables))
	hiddenSchemaCount := report.InvisibleColumnCount + report.InvisibleIndexCount + report.GIPKTableCount
	hiddenSchemaSeverity := "warn"
	if invisibleGIPKIncompatible(source, dest, report) {
		hiddenSchemaSeverity = "error"
	}
	findings = append(findings, compat.Finding{
		Code:     "invisible_gipk_features_detected",
		Severity: hiddenSchemaSeverity,
		Message: fmt.Sprintf(
			"Detected %d hidden-schema feature(s) in source scope: invisible_columns=%d invisible_indexes=%d gipk_tables=%d.",
			hiddenSchemaCount,
			report.InvisibleColumnCount,
			report.InvisibleIndexCount,
			report.GIPKTableCount,
		),
		Proposal: invisibleGIPKSummaryProposal(dest, report),
	})

	invisibleSeverity := "info"
	if invisibleFeatureDriftsOnDestination(dest, report) {
		invisibleSeverity = "error"
	}
	for _, issue := range report.InvisibleColumns {
		findings = append(findings, compat.Finding{
			Code:     "invisible_column_detected",
			Severity: invisibleSeverity,
			Message:  fmt.Sprintf("Source %s.%s column %s is invisible (%s).", issue.Database, issue.Table, issue.Column, issue.Extra),
			Proposal: issue.Proposal,
		})
	}
	for _, issue := range report.InvisibleIndexes {
		findings = append(findings, compat.Finding{
			Code:     "invisible_index_detected",
			Severity: invisibleSeverity,
			Message:  fmt.Sprintf("Source %s.%s index %s is invisible.", issue.Database, issue.Table, issue.Index),
			Proposal: issue.Proposal,
		})
	}

	gipkSeverity := "warn"
	if gipkDriftsOnDestination(dest, report) {
		gipkSeverity = "error"
	}
	for _, issue := range report.GIPKTables {
		findings = append(findings, compat.Finding{
			Code:     "generated_invisible_primary_key_detected",
			Severity: gipkSeverity,
			Message:  fmt.Sprintf("Source %s.%s relies on invisible primary-key column %s.", issue.Database, issue.Table, issue.Column),
			Proposal: issue.Proposal,
		})
	}
	return findings
}

func invisibleGIPKSummaryProposal(dest compat.Instance, report invisibleGIPKPrecheckReport) string {
	if invisibleGIPKIncompatible(compat.Instance{}, dest, report) {
		switch dest.Engine {
		case compat.EngineMariaDB:
			return "Destination MariaDB does not preserve MySQL invisible-column, invisible-index, or GIPK semantics. Materialize hidden schema features explicitly before migrate, or keep this path blocked."
		case compat.EngineMySQL:
			return "Destination MySQL line is too old for at least one hidden-schema feature in source. Upgrade the destination or remove hidden-schema features before rerunning plan/migrate."
		default:
			return "Destination support for hidden-schema features is unknown. Do not proceed until invisible-column and GIPK behavior is proven for this target."
		}
	}
	if report.GIPKTableCount > 0 {
		return "Keep dump/import mode explicit for GIPK tables. Supported MySQL targets preserve hidden PKs when included, while --skip-generated-invisible-primary-key removes them entirely from logical dumps."
	}
	return "Destination appears to preserve the hidden-schema features found in source. Keep rehearsal evidence anyway so downgrade behavior stays explicit."
}

func invisibleGIPKIncompatible(source compat.Instance, dest compat.Instance, report invisibleGIPKPrecheckReport) bool {
	return invisibleFeatureDriftsOnDestination(dest, report) || gipkDriftsOnDestination(dest, report)
}

func invisibleFeatureDriftsOnDestination(dest compat.Instance, report invisibleGIPKPrecheckReport) bool {
	if report.InvisibleColumnCount == 0 && report.InvisibleIndexCount == 0 {
		return false
	}
	if report.InvisibleColumnCount > 0 && !destinationSupportsInvisibleColumns(dest) {
		return true
	}
	if report.InvisibleIndexCount > 0 && !destinationSupportsInvisibleIndexes(dest) {
		return true
	}
	return false
}

func gipkDriftsOnDestination(dest compat.Instance, report invisibleGIPKPrecheckReport) bool {
	if report.GIPKTableCount == 0 {
		return false
	}
	return !destinationSupportsGIPK(dest)
}

func destinationSupportsInvisibleColumns(dest compat.Instance) bool {
	if dest.Engine == compat.EngineMySQL {
		return versionAtLeast(dest, 8, 0, 23)
	}
	if dest.Engine == compat.EngineMariaDB {
		return versionAtLeast(dest, 10, 3, 3)
	}
	return false
}

func destinationSupportsInvisibleIndexes(dest compat.Instance) bool {
	return dest.Engine == compat.EngineMySQL && versionAtLeast(dest, 8, 0, 0)
}

func destinationSupportsGIPK(dest compat.Instance) bool {
	return dest.Engine == compat.EngineMySQL && versionAtLeast(dest, 8, 0, 30)
}

func versionAtLeast(instance compat.Instance, major int, minor int, patch int) bool {
	if !instance.Parsed {
		return false
	}
	if instance.Major != major {
		return instance.Major > major
	}
	if instance.Minor != minor {
		return instance.Minor > minor
	}
	return instance.Patch >= patch
}

func queryOptionalBooleanVariable(ctx context.Context, db *sql.DB, expression string) (bool, bool, error) {
	var raw sql.NullString
	query := fmt.Sprintf("SELECT %s", expression)
	if err := db.QueryRowContext(ctx, query).Scan(&raw); err != nil {
		return false, false, err
	}
	value := strings.TrimSpace(strings.ToLower(raw.String))
	switch value {
	case "1", "on", "true":
		return true, true, nil
	case "0", "off", "false":
		return false, true, nil
	default:
		return false, true, nil
	}
}

func sqlPlaceholders(values []string) (string, []any) {
	placeholders := make([]string, 0, len(values))
	args := make([]any, 0, len(values))
	for _, value := range values {
		placeholders = append(placeholders, "?")
		args = append(args, value)
	}
	return strings.Join(placeholders, ","), args
}

func writeInvisibleGIPKPrecheckReport(out io.Writer, cfg config.RuntimeConfig, report invisibleGIPKPrecheckReport) error {
	payload := invisibleGIPKPrecheckResult{
		Command:   "migrate",
		Status:    "incompatible",
		Precheck:  report,
		Timestamp: time.Now().UTC(),
		Version:   version.Version,
	}
	if cfg.JSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	if _, err := fmt.Fprintf(
		out,
		"[migrate] status=incompatible precheck=%s source_version=%q dest_version=%q invisible_columns=%d invisible_indexes=%d gipk_tables=%d findings=%d\n",
		report.Name,
		report.SourceVersion,
		report.DestVersion,
		report.InvisibleColumnCount,
		report.InvisibleIndexCount,
		report.GIPKTableCount,
		len(report.Findings),
	); err != nil {
		return err
	}
	for _, finding := range report.Findings {
		if _, err := fmt.Fprintf(
			out,
			"[migrate] precheck %s code=%s message=%s proposal=%s\n",
			finding.Severity,
			finding.Code,
			finding.Message,
			finding.Proposal,
		); err != nil {
			return err
		}
	}
	return nil
}
