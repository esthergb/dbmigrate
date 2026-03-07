package commands

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/esthergb/dbmigrate/internal/compat"
	"github.com/esthergb/dbmigrate/internal/config"
	"github.com/esthergb/dbmigrate/internal/schema"
	"github.com/esthergb/dbmigrate/internal/state"
	"github.com/esthergb/dbmigrate/internal/version"
)

type collationIssue struct {
	Scope      string `json:"scope"`
	Database   string `json:"database,omitempty"`
	Table      string `json:"table,omitempty"`
	Column     string `json:"column,omitempty"`
	Collation  string `json:"collation"`
	Proposal   string `json:"proposal"`
	RiskDetail string `json:"risk_detail,omitempty"`
}

type collationPrecheckReport struct {
	Name                         string           `json:"name"`
	Incompatible                 bool             `json:"incompatible"`
	SourceVersion                string           `json:"source_version"`
	DestVersion                  string           `json:"dest_version"`
	SourceServerCollation        string           `json:"source_server_collation,omitempty"`
	DestServerCollation          string           `json:"dest_server_collation,omitempty"`
	DestinationSupportedCount    int              `json:"destination_supported_collation_count"`
	UnsupportedDestinationCount  int              `json:"unsupported_destination_count"`
	ClientCompatibilityRiskCount int              `json:"client_compatibility_risk_count"`
	UnsupportedDestination       []collationIssue `json:"unsupported_destination,omitempty"`
	ClientCompatibilityRisks     []collationIssue `json:"client_compatibility_risks,omitempty"`
	Findings                     []compat.Finding `json:"findings,omitempty"`
}

type migrateCollationPrecheckResult struct {
	Command   string                  `json:"command"`
	Status    string                  `json:"status"`
	Precheck  collationPrecheckReport `json:"precheck"`
	Timestamp time.Time               `json:"timestamp"`
	Version   string                  `json:"version"`
}

func runCollationPrecheck(
	ctx context.Context,
	source *sql.DB,
	dest *sql.DB,
	stateDir string,
	includeDatabases []string,
	excludeDatabases []string,
) (collationPrecheckReport, error) {
	report := collationPrecheckReport{
		Name: "collation-compatibility",
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

	report.SourceServerCollation, _ = queryServerCollation(ctx, source)
	report.DestServerCollation, _ = queryServerCollation(ctx, dest)

	supportedCollations, err := queryDestinationSupportedCollations(ctx, dest)
	if err != nil {
		return report, fmt.Errorf("query destination supported collations: %w", err)
	}
	report.DestinationSupportedCount = len(supportedCollations)

	allDatabases, err := listDatabases(ctx, source)
	if err != nil {
		return report, fmt.Errorf("list source databases: %w", err)
	}
	selectedDatabases := schema.SelectDatabases(allDatabases, includeDatabases, excludeDatabases)
	if len(selectedDatabases) == 0 {
		if err := cleanupCollationPrecheckArtifact(stateDir); err != nil {
			return report, err
		}
		report.Findings = append(report.Findings, compat.Finding{
			Code:     "collation_precheck_no_selected_databases",
			Severity: "info",
			Message:  "No source databases selected for collation inventory.",
			Proposal: "Set --databases or verify include/exclude filters if collation compatibility should be checked for a partial migration scope.",
		})
		return report, nil
	}

	sourceItems, err := querySourceCollationItems(ctx, source, selectedDatabases)
	if err != nil {
		return report, err
	}

	report.UnsupportedDestination = detectUnsupportedDestinationCollations(sourceItems, supportedCollations)
	report.ClientCompatibilityRisks = detectClientCompatibilityRisks(
		sourceItems,
		report.SourceServerCollation,
		report.DestServerCollation,
	)
	report.UnsupportedDestinationCount = len(report.UnsupportedDestination)
	report.ClientCompatibilityRiskCount = len(report.ClientCompatibilityRisks)
	report.Incompatible = report.UnsupportedDestinationCount > 0
	report.Findings = buildCollationPrecheckFindings(report)

	if err := persistCollationPrecheckArtifact(stateDir, report); err != nil {
		return report, err
	}
	return report, nil
}

func queryServerCollation(ctx context.Context, db *sql.DB) (string, error) {
	var out sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT @@collation_server").Scan(&out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String), nil
}

func queryDestinationSupportedCollations(ctx context.Context, db *sql.DB) (map[string]struct{}, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT COLLATION_NAME
		FROM information_schema.COLLATIONS
		WHERE COALESCE(COLLATION_NAME, '') <> ''
	`)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make(map[string]struct{}, 256)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		name = strings.TrimSpace(strings.ToLower(name))
		if name == "" {
			continue
		}
		out[name] = struct{}{}
		if strings.HasPrefix(name, "uca1400_") {
			out["utf8mb4_"+name] = struct{}{}
		}
		if strings.HasPrefix(name, "utf8mb4_uca1400_") {
			out[strings.TrimPrefix(name, "utf8mb4_")] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func querySourceCollationItems(ctx context.Context, db *sql.DB, databases []string) ([]collationIssue, error) {
	placeholders, args := sqlPlaceholders(databases)

	schemaRows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT SCHEMA_NAME, DEFAULT_COLLATION_NAME
		FROM information_schema.SCHEMATA
		WHERE SCHEMA_NAME IN (%s)
		  AND COALESCE(DEFAULT_COLLATION_NAME, '') <> ''
		ORDER BY SCHEMA_NAME
	`, placeholders), args...)
	if err != nil {
		return nil, fmt.Errorf("query source schema collations: %w", err)
	}
	defer func() {
		_ = schemaRows.Close()
	}()

	issues := make([]collationIssue, 0, 32)
	for schemaRows.Next() {
		var issue collationIssue
		issue.Scope = "schema"
		if err := schemaRows.Scan(&issue.Database, &issue.Collation); err != nil {
			return nil, err
		}
		issue.Proposal = collationProposal(issue.Collation)
		issues = append(issues, issue)
	}
	if err := schemaRows.Err(); err != nil {
		return nil, err
	}

	tableRows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT TABLE_SCHEMA, TABLE_NAME, TABLE_COLLATION
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA IN (%s)
		  AND COALESCE(TABLE_COLLATION, '') <> ''
		ORDER BY TABLE_SCHEMA, TABLE_NAME
	`, placeholders), args...)
	if err != nil {
		return nil, fmt.Errorf("query source table collations: %w", err)
	}
	defer func() {
		_ = tableRows.Close()
	}()

	for tableRows.Next() {
		var issue collationIssue
		issue.Scope = "table"
		if err := tableRows.Scan(&issue.Database, &issue.Table, &issue.Collation); err != nil {
			return nil, err
		}
		issue.Proposal = collationProposal(issue.Collation)
		issues = append(issues, issue)
	}
	if err := tableRows.Err(); err != nil {
		return nil, err
	}

	columnRows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, COLLATION_NAME
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA IN (%s)
		  AND COALESCE(COLLATION_NAME, '') <> ''
		ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION
	`, placeholders), args...)
	if err != nil {
		return nil, fmt.Errorf("query source column collations: %w", err)
	}
	defer func() {
		_ = columnRows.Close()
	}()

	for columnRows.Next() {
		var issue collationIssue
		issue.Scope = "column"
		if err := columnRows.Scan(&issue.Database, &issue.Table, &issue.Column, &issue.Collation); err != nil {
			return nil, err
		}
		issue.Proposal = collationProposal(issue.Collation)
		issues = append(issues, issue)
	}
	if err := columnRows.Err(); err != nil {
		return nil, err
	}

	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Scope != issues[j].Scope {
			return issues[i].Scope < issues[j].Scope
		}
		if issues[i].Database != issues[j].Database {
			return issues[i].Database < issues[j].Database
		}
		if issues[i].Table != issues[j].Table {
			return issues[i].Table < issues[j].Table
		}
		if issues[i].Column != issues[j].Column {
			return issues[i].Column < issues[j].Column
		}
		return issues[i].Collation < issues[j].Collation
	})

	return issues, nil
}

func detectUnsupportedDestinationCollations(sourceItems []collationIssue, supported map[string]struct{}) []collationIssue {
	out := make([]collationIssue, 0, 8)
	for _, item := range sourceItems {
		name := strings.TrimSpace(strings.ToLower(item.Collation))
		if name == "" {
			continue
		}
		if _, ok := supported[name]; ok {
			continue
		}
		item.RiskDetail = "server_unsupported"
		out = append(out, item)
	}
	return dedupeCollationIssues(out)
}

func detectClientCompatibilityRisks(sourceItems []collationIssue, sourceServerCollation string, destServerCollation string) []collationIssue {
	riskItems := make([]collationIssue, 0, 8)
	for _, item := range sourceItems {
		if !collationHasClientCompatibilityRisk(item.Collation) {
			continue
		}
		item.RiskDetail = "client_compatibility"
		item.Proposal = clientCompatibilityProposal(item.Collation)
		riskItems = append(riskItems, item)
	}
	if collationHasClientCompatibilityRisk(sourceServerCollation) {
		riskItems = append(riskItems, collationIssue{
			Scope:      "source-server",
			Collation:  sourceServerCollation,
			Proposal:   clientCompatibilityProposal(sourceServerCollation),
			RiskDetail: "client_compatibility",
		})
	}
	if collationHasClientCompatibilityRisk(destServerCollation) {
		riskItems = append(riskItems, collationIssue{
			Scope:      "dest-server",
			Collation:  destServerCollation,
			Proposal:   clientCompatibilityProposal(destServerCollation),
			RiskDetail: "client_compatibility",
		})
	}
	return dedupeCollationIssues(riskItems)
}

func dedupeCollationIssues(items []collationIssue) []collationIssue {
	seen := make(map[string]struct{}, len(items))
	out := make([]collationIssue, 0, len(items))
	for _, item := range items {
		key := strings.Join([]string{item.Scope, item.Database, item.Table, item.Column, item.Collation, item.RiskDetail}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func collationHasClientCompatibilityRisk(name string) bool {
	value := strings.TrimSpace(strings.ToLower(name))
	return strings.HasPrefix(value, "utf8mb4_uca1400_") || strings.HasPrefix(value, "uca1400_")
}

func collationProposal(collation string) string {
	name := strings.TrimSpace(strings.ToLower(collation))
	switch {
	case strings.HasPrefix(name, "utf8mb4_0900_"):
		return "Map this MySQL 8.0/8.4 collation to an approved MariaDB-supported equivalent before cross-engine migration, and document the sort/comparison impact."
	case strings.HasPrefix(name, "utf8mb4_uca1400_") || strings.HasPrefix(name, "uca1400_"):
		return "Map this MariaDB UCA1400 collation to an approved MySQL-supported equivalent before cross-engine migration, and document the sort/comparison impact."
	default:
		return "Choose an equivalent collation supported by the destination and record the semantic change before migration."
	}
}

func clientCompatibilityProposal(collation string) string {
	return fmt.Sprintf(
		"Representative CLIs may still connect, but application stacks can lag behind %s. Record driver/library versions, rehearse connection startup, and choose a mutually understood connection collation if needed.",
		collation,
	)
}

func buildCollationPrecheckFindings(report collationPrecheckReport) []compat.Finding {
	findings := make([]compat.Finding, 0, 1+len(report.UnsupportedDestination)+len(report.ClientCompatibilityRisks))

	if report.UnsupportedDestinationCount > 0 {
		findings = append(findings, compat.Finding{
			Code:     "unsupported_destination_collations_detected",
			Severity: "error",
			Message: fmt.Sprintf(
				"Detected %d source collation reference(s) that are unsupported on the destination server.",
				report.UnsupportedDestinationCount,
			),
			Proposal: "Rewrite unsupported collations to approved destination equivalents before migrate. Treat this as a server-side incompatibility, not a client quirk.",
		})
		for _, issue := range report.UnsupportedDestination {
			findings = append(findings, compat.Finding{
				Code:     "unsupported_destination_collation",
				Severity: "error",
				Message:  describeCollationIssue(issue, "Destination server does not support"),
				Proposal: issue.Proposal,
			})
		}
	}

	if report.ClientCompatibilityRiskCount > 0 {
		findings = append(findings, compat.Finding{
			Code:     "client_collation_compatibility_risk_detected",
			Severity: "warn",
			Message: fmt.Sprintf(
				"Detected %d collation reference(s) that are known client-compatibility risks even when the server itself accepts them.",
				report.ClientCompatibilityRiskCount,
			),
			Proposal: "Separate server-side support from client/library risk in cutover planning. Rehearse representative clients and record connection-init assumptions.",
		})
		for _, issue := range report.ClientCompatibilityRisks {
			findings = append(findings, compat.Finding{
				Code:     "client_collation_compatibility_risk",
				Severity: "warn",
				Message:  describeCollationIssue(issue, "Server accepts but some clients may not understand"),
				Proposal: issue.Proposal,
			})
		}
	}

	if len(findings) == 0 {
		findings = append(findings, compat.Finding{
			Code:     "collation_inventory_clean",
			Severity: "info",
			Message:  "Selected source databases did not reveal unsupported destination collations or known client-compatibility collation families.",
			Proposal: "Keep collation inventory artifacts anyway. Connection-time and schema-time collation failures should stay explicit in cutover evidence.",
		})
	}
	return findings
}

func describeCollationIssue(issue collationIssue, prefix string) string {
	location := issue.Scope
	switch issue.Scope {
	case "schema":
		location = fmt.Sprintf("schema %s", issue.Database)
	case "table":
		location = fmt.Sprintf("table %s.%s", issue.Database, issue.Table)
	case "column":
		location = fmt.Sprintf("column %s.%s.%s", issue.Database, issue.Table, issue.Column)
	case "source-server":
		location = "source server default collation"
	case "dest-server":
		location = "destination server default collation"
	}
	return fmt.Sprintf("%s %s collation %q.", prefix, location, issue.Collation)
}

func collationPrecheckArtifactPath(stateDir string) string {
	baseDir := strings.TrimSpace(stateDir)
	if baseDir == "" {
		baseDir = "./state"
	}
	return filepath.Join(baseDir, "collation-precheck.json")
}

func persistCollationPrecheckArtifact(stateDir string, report collationPrecheckReport) error {
	if len(report.Findings) == 0 && report.UnsupportedDestinationCount == 0 && report.ClientCompatibilityRiskCount == 0 {
		return cleanupCollationPrecheckArtifact(stateDir)
	}

	path := collationPrecheckArtifactPath(stateDir)
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal collation precheck artifact: %w", err)
	}
	if err := state.WritePrivateFileAtomic(path, append(raw, '\n')); err != nil {
		return fmt.Errorf("write collation precheck artifact: %w", err)
	}
	return nil
}

func cleanupCollationPrecheckArtifact(stateDir string) error {
	path := collationPrecheckArtifactPath(stateDir)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cleanup collation precheck artifact %s: %w", path, err)
	}
	return nil
}

func loadCollationPrecheckArtifact(stateDir string) (collationPrecheckReport, error) {
	path := collationPrecheckArtifactPath(stateDir)
	raw, err := os.ReadFile(path)
	if err != nil {
		return collationPrecheckReport{}, fmt.Errorf("read collation precheck artifact: %w", err)
	}
	var report collationPrecheckReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return collationPrecheckReport{}, fmt.Errorf("parse collation precheck artifact: %w", err)
	}
	return report, nil
}

func writeCollationMigratePrecheckReport(out io.Writer, cfg config.RuntimeConfig, report collationPrecheckReport) error {
	payload := migrateCollationPrecheckResult{
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
		"[migrate] status=incompatible precheck=%s source_server_collation=%q dest_server_collation=%q unsupported_destination=%d client_risks=%d findings=%d\n",
		report.Name,
		report.SourceServerCollation,
		report.DestServerCollation,
		report.UnsupportedDestinationCount,
		report.ClientCompatibilityRiskCount,
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
