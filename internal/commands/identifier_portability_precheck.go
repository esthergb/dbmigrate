package commands

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/esthergb/dbmigrate/internal/compat"
	"github.com/esthergb/dbmigrate/internal/config"
	"github.com/esthergb/dbmigrate/internal/version"
)

type identifierPortabilityPrecheckReport struct {
	Name                      string                     `json:"name"`
	Incompatible              bool                       `json:"incompatible"`
	SourceVersion             string                     `json:"source_version"`
	DestVersion               string                     `json:"dest_version"`
	SourceSQLMode             string                     `json:"source_sql_mode,omitempty"`
	DestSQLMode               string                     `json:"dest_sql_mode,omitempty"`
	SourceLowerCaseKnown      bool                       `json:"source_lower_case_known"`
	SourceLowerCaseTableNames int                        `json:"source_lower_case_table_names,omitempty"`
	DestLowerCaseKnown        bool                       `json:"dest_lower_case_known"`
	DestLowerCaseTableNames   int                        `json:"dest_lower_case_table_names,omitempty"`
	SourceReservedWordSource  string                     `json:"source_reserved_word_source,omitempty"`
	DestReservedWordSource    string                     `json:"dest_reserved_word_source,omitempty"`
	ReservedIdentifierCount   int                        `json:"reserved_identifier_count"`
	ViewParserRiskCount       int                        `json:"view_parser_risk_count"`
	CaseCollisionCount        int                        `json:"case_collision_count"`
	MixedCaseIdentifierCount  int                        `json:"mixed_case_identifier_count"`
	ReservedIdentifiers       []reservedIdentifierIssue  `json:"reserved_identifiers,omitempty"`
	ViewParserRisks           []viewParserRiskIssue      `json:"view_parser_risks,omitempty"`
	CaseCollisions            []caseCollisionIssue       `json:"case_collisions,omitempty"`
	MixedCaseIdentifiers      []mixedCaseIdentifierIssue `json:"mixed_case_identifiers,omitempty"`
	Findings                  []compat.Finding           `json:"findings,omitempty"`
}

type identifierPortabilityPrecheckResult struct {
	Command   string                              `json:"command"`
	Status    string                              `json:"status"`
	Precheck  identifierPortabilityPrecheckReport `json:"precheck"`
	Timestamp time.Time                           `json:"timestamp"`
	Version   string                              `json:"version"`
}

type reservedIdentifierIssue struct {
	Database        string `json:"database"`
	ObjectType      string `json:"object_type"`
	ObjectName      string `json:"object_name"`
	Identifier      string `json:"identifier"`
	Reason          string `json:"reason"`
	DestinationOnly bool   `json:"destination_only"`
	Proposal        string `json:"proposal"`
}

type viewParserRiskIssue struct {
	Database string `json:"database"`
	View     string `json:"view"`
	Risk     string `json:"risk"`
	Proposal string `json:"proposal"`
}

type caseCollisionIssue struct {
	Database   string   `json:"database,omitempty"`
	ObjectType string   `json:"object_type"`
	FoldedName string   `json:"folded_name"`
	Objects    []string `json:"objects"`
	Proposal   string   `json:"proposal"`
}

type mixedCaseIdentifierIssue struct {
	Database   string `json:"database,omitempty"`
	ObjectType string `json:"object_type"`
	ObjectName string `json:"object_name"`
	Proposal   string `json:"proposal"`
}

type inventoryObject struct {
	Database   string
	ObjectType string
	ObjectName string
}

type inventoryColumn struct {
	Database string
	Table    string
	Column   string
}

type viewDefinition struct {
	Database string
	View     string
	Create   string
}

func runIdentifierPortabilityPrecheck(
	ctx context.Context,
	source *sql.DB,
	dest *sql.DB,
	sourceInstance compat.Instance,
	destInstance compat.Instance,
	includeDatabases []string,
	excludeDatabases []string,
) (identifierPortabilityPrecheckReport, error) {
	report := identifierPortabilityPrecheckReport{
		Name:          "identifier-portability",
		SourceVersion: sourceInstance.RawVersion,
		DestVersion:   destInstance.RawVersion,
	}
	if source == nil || dest == nil {
		return report, fmt.Errorf("source and destination connections are required")
	}

	databases, err := listSelectableDatabases(ctx, source, includeDatabases, excludeDatabases)
	if err != nil {
		return report, err
	}
	if len(databases) == 0 {
		report.Findings = []compat.Finding{{
			Code:     "identifier_portability_no_selected_databases",
			Severity: "info",
			Message:  "No user databases selected for identifier portability inventory.",
			Proposal: "Use --databases to narrow scope or rerun without filters to inspect all user schemas.",
		}}
		return report, nil
	}

	sourceSQLMode, err := querySQLMode(ctx, source)
	if err != nil {
		report.Findings = append(report.Findings, compat.Finding{
			Code:     "identifier_portability_source_sql_mode_unavailable",
			Severity: "warn",
			Message:  fmt.Sprintf("Unable to read source sql_mode during identifier portability precheck: %v.", err),
			Proposal: "Review source SQL mode manually before trusting parser portability for views.",
		})
	} else {
		report.SourceSQLMode = sourceSQLMode
	}

	destSQLMode, err := querySQLMode(ctx, dest)
	if err != nil {
		report.Findings = append(report.Findings, compat.Finding{
			Code:     "identifier_portability_dest_sql_mode_unavailable",
			Severity: "warn",
			Message:  fmt.Sprintf("Unable to read destination sql_mode during identifier portability precheck: %v.", err),
			Proposal: "Review destination SQL mode manually before trusting parser portability for views.",
		})
	} else {
		report.DestSQLMode = destSQLMode
	}

	if value, known, err := queryOptionalIntVariable(ctx, source, "@@lower_case_table_names"); err != nil {
		report.Findings = append(report.Findings, compat.Finding{
			Code:     "source_lower_case_table_names_unavailable",
			Severity: "warn",
			Message:  fmt.Sprintf("Unable to read source @@lower_case_table_names: %v.", err),
			Proposal: "Record source lower_case_table_names manually before claiming name portability.",
		})
	} else if known {
		report.SourceLowerCaseKnown = true
		report.SourceLowerCaseTableNames = value
	}

	if value, known, err := queryOptionalIntVariable(ctx, dest, "@@lower_case_table_names"); err != nil {
		report.Findings = append(report.Findings, compat.Finding{
			Code:     "dest_lower_case_table_names_unavailable",
			Severity: "warn",
			Message:  fmt.Sprintf("Unable to read destination @@lower_case_table_names: %v.", err),
			Proposal: "Record destination lower_case_table_names manually before claiming name portability.",
		})
	} else if known {
		report.DestLowerCaseKnown = true
		report.DestLowerCaseTableNames = value
	}

	sourceReservedWords, sourceReservedSource, sourceReservedErr := queryReservedWords(ctx, source, sourceInstance)
	if sourceReservedErr != nil {
		report.Findings = append(report.Findings, compat.Finding{
			Code:     "source_reserved_word_inventory_fallback",
			Severity: "warn",
			Message:  fmt.Sprintf("Fell back to built-in source reserved-word watchlist: %v.", sourceReservedErr),
			Proposal: "Grant access to INFORMATION_SCHEMA.KEYWORDS or validate reserved-word drift manually for exact coverage.",
		})
	}
	report.SourceReservedWordSource = sourceReservedSource

	destReservedWords, destReservedSource, destReservedErr := queryReservedWords(ctx, dest, destInstance)
	if destReservedErr != nil {
		report.Findings = append(report.Findings, compat.Finding{
			Code:     "dest_reserved_word_inventory_fallback",
			Severity: "warn",
			Message:  fmt.Sprintf("Fell back to built-in destination reserved-word watchlist: %v.", destReservedErr),
			Proposal: "Grant access to INFORMATION_SCHEMA.KEYWORDS or validate reserved-word drift manually for exact coverage.",
		})
	}
	report.DestReservedWordSource = destReservedSource

	objects, err := queryInventoryObjects(ctx, source, databases)
	if err != nil {
		return report, err
	}
	columns, err := queryInventoryColumns(ctx, source, databases)
	if err != nil {
		return report, err
	}
	views, err := queryViewDefinitions(ctx, source, databases)
	if err != nil {
		return report, err
	}

	report.ReservedIdentifiers = detectReservedIdentifierIssues(databases, objects, columns, sourceReservedWords, destReservedWords)
	report.ReservedIdentifierCount = len(report.ReservedIdentifiers)
	report.CaseCollisions = detectCaseCollisionIssues(databases, objects)
	report.CaseCollisionCount = len(report.CaseCollisions)
	report.MixedCaseIdentifiers = detectMixedCaseIdentifierIssues(databases, objects, report.SourceLowerCaseKnown, report.SourceLowerCaseTableNames, report.DestLowerCaseKnown, report.DestLowerCaseTableNames)
	report.MixedCaseIdentifierCount = len(report.MixedCaseIdentifiers)
	report.ViewParserRisks = detectViewParserRiskIssues(views, report.SourceSQLMode, report.DestSQLMode)
	report.ViewParserRiskCount = len(report.ViewParserRisks)

	report.Findings = append(report.Findings, buildIdentifierPortabilityFindings(report)...)
	for _, finding := range report.Findings {
		if finding.Severity == "error" {
			report.Incompatible = true
			break
		}
	}
	return report, nil
}

func queryOptionalIntVariable(ctx context.Context, db *sql.DB, expression string) (int, bool, error) {
	var raw sql.NullInt64
	query := fmt.Sprintf("SELECT %s", expression)
	if err := db.QueryRowContext(ctx, query).Scan(&raw); err == nil {
		return int(raw.Int64), true, nil
	}
	var fallback sql.NullString
	if err := db.QueryRowContext(ctx, query).Scan(&fallback); err != nil {
		return 0, false, err
	}
	value := strings.TrimSpace(fallback.String)
	if value == "" {
		return 0, true, nil
	}
	switch strings.ToLower(value) {
	case "off", "false":
		return 0, true, nil
	case "on", "true":
		return 1, true, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, false, err
	}
	return parsed, true, nil
}

func queryReservedWords(ctx context.Context, db *sql.DB, instance compat.Instance) (map[string]struct{}, string, error) {
	rows, err := db.QueryContext(ctx, "SELECT WORD, RESERVED FROM information_schema.KEYWORDS ORDER BY WORD")
	if err == nil {
		defer func() { _ = rows.Close() }()
		out := map[string]struct{}{}
		for rows.Next() {
			var word string
			var reserved sql.NullString
			if err := rows.Scan(&word, &reserved); err != nil {
				return nil, "", err
			}
			if reservedWordEnabled(reserved.String) {
				out[strings.ToUpper(strings.TrimSpace(word))] = struct{}{}
			}
		}
		if err := rows.Err(); err != nil {
			return nil, "", err
		}
		if len(out) > 0 {
			return out, "information_schema.KEYWORDS", nil
		}
	}

	fallback := fallbackReservedWords(instance)
	if len(fallback) == 0 {
		if err == nil {
			err = fmt.Errorf("reserved-word inventory unavailable")
		}
		return map[string]struct{}{}, "", err
	}
	if err == nil {
		err = fmt.Errorf("reserved-word inventory unavailable")
	}
	return fallback, "built-in watchlist", err
}

func reservedWordEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "y", "yes", "true":
		return true
	default:
		return false
	}
}

func fallbackReservedWords(instance compat.Instance) map[string]struct{} {
	words := []string{
		"CUBE", "CUME_DIST", "DENSE_RANK", "EMPTY", "EXCEPT", "FIRST_VALUE", "FUNCTION",
		"GROUPS", "INTERSECT", "JSON_TABLE", "LAG", "LAST_VALUE", "LATERAL", "LEAD",
		"NTH_VALUE", "NTILE", "OF", "OVER", "PERCENT_RANK", "RANK", "RECURSIVE",
		"ROW_NUMBER", "SEQUENCE", "SYSTEM", "WINDOW",
	}
	if instance.Engine == compat.EngineMySQL && instance.Major >= 9 {
		words = append(words, "LIBRARY")
	}
	out := make(map[string]struct{}, len(words))
	for _, word := range words {
		out[word] = struct{}{}
	}
	return out
}

func queryInventoryObjects(ctx context.Context, source *sql.DB, databases []string) ([]inventoryObject, error) {
	placeholders, args := sqlPlaceholders(databases)
	rows, err := source.QueryContext(ctx, fmt.Sprintf(`
		SELECT TABLE_SCHEMA, TABLE_NAME, TABLE_TYPE
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA IN (%s)
		ORDER BY TABLE_SCHEMA, TABLE_NAME
	`, placeholders), args...)
	if err != nil {
		return nil, fmt.Errorf("query schema objects for identifier portability: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]inventoryObject, 0, 16+len(databases))
	for _, databaseName := range databases {
		out = append(out, inventoryObject{Database: databaseName, ObjectType: "database", ObjectName: databaseName})
	}
	for rows.Next() {
		var databaseName string
		var objectName string
		var tableType string
		if err := rows.Scan(&databaseName, &objectName, &tableType); err != nil {
			return nil, err
		}
		objectType := "table"
		if strings.EqualFold(strings.TrimSpace(tableType), "VIEW") {
			objectType = "view"
		}
		out = append(out, inventoryObject{Database: databaseName, ObjectType: objectType, ObjectName: objectName})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func queryInventoryColumns(ctx context.Context, source *sql.DB, databases []string) ([]inventoryColumn, error) {
	placeholders, args := sqlPlaceholders(databases)
	rows, err := source.QueryContext(ctx, fmt.Sprintf(`
		SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA IN (%s)
		ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION
	`, placeholders), args...)
	if err != nil {
		return nil, fmt.Errorf("query schema columns for identifier portability: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]inventoryColumn, 0, 32)
	for rows.Next() {
		var item inventoryColumn
		if err := rows.Scan(&item.Database, &item.Table, &item.Column); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func queryViewDefinitions(ctx context.Context, source *sql.DB, databases []string) ([]viewDefinition, error) {
	objects, err := queryInventoryObjects(ctx, source, databases)
	if err != nil {
		return nil, err
	}
	out := make([]viewDefinition, 0, 8)
	for _, object := range objects {
		if object.ObjectType != "view" {
			continue
		}
		stmt, err := fetchShowCreateStatement(ctx, source, fmt.Sprintf("SHOW CREATE VIEW %s.%s", quoteIdentifier(object.Database), quoteIdentifier(object.ObjectName)))
		if err != nil {
			return nil, err
		}
		out = append(out, viewDefinition{Database: object.Database, View: object.ObjectName, Create: stmt})
	}
	return out, nil
}

func fetchShowCreateStatement(ctx context.Context, db *sql.DB, query string) (string, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return "", err
	}
	if len(columns) < 2 {
		return "", fmt.Errorf("unexpected SHOW CREATE result format")
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", err
		}
		return "", sql.ErrNoRows
	}
	values := make([]sql.RawBytes, len(columns))
	scanArgs := make([]any, len(columns))
	for i := range values {
		scanArgs[i] = &values[i]
	}
	if err := rows.Scan(scanArgs...); err != nil {
		return "", err
	}
	return string(values[1]), nil
}

func detectReservedIdentifierIssues(databases []string, objects []inventoryObject, columns []inventoryColumn, sourceReservedWords map[string]struct{}, destReservedWords map[string]struct{}) []reservedIdentifierIssue {
	issues := make([]reservedIdentifierIssue, 0, 8)
	for _, object := range objects {
		if object.ObjectType == "database" {
			if issue, ok := reservedIdentifierIssueForToken(object.Database, "database", object.ObjectName, object.ObjectName, sourceReservedWords, destReservedWords); ok {
				issues = append(issues, issue)
			}
			continue
		}
		if issue, ok := reservedIdentifierIssueForToken(object.Database, object.ObjectType, object.ObjectName, object.ObjectName, sourceReservedWords, destReservedWords); ok {
			issues = append(issues, issue)
		}
	}
	for _, column := range columns {
		if issue, ok := reservedIdentifierIssueForToken(column.Database, "column", column.Table, column.Column, sourceReservedWords, destReservedWords); ok {
			issues = append(issues, issue)
		}
	}
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Database != issues[j].Database {
			return issues[i].Database < issues[j].Database
		}
		if issues[i].ObjectType != issues[j].ObjectType {
			return issues[i].ObjectType < issues[j].ObjectType
		}
		if issues[i].ObjectName != issues[j].ObjectName {
			return issues[i].ObjectName < issues[j].ObjectName
		}
		return issues[i].Identifier < issues[j].Identifier
	})
	return issues
}

func reservedIdentifierIssueForToken(databaseName string, objectType string, objectName string, identifier string, sourceReservedWords map[string]struct{}, destReservedWords map[string]struct{}) (reservedIdentifierIssue, bool) {
	token := strings.ToUpper(strings.TrimSpace(identifier))
	if token == "" {
		return reservedIdentifierIssue{}, false
	}
	if _, ok := destReservedWords[token]; !ok {
		return reservedIdentifierIssue{}, false
	}
	_, sourceReserved := sourceReservedWords[token]
	reason := fmt.Sprintf("Identifier %q is reserved on both the source and destination parser inventories.", identifier)
	if !sourceReserved {
		reason = fmt.Sprintf("Identifier %q becomes reserved on the destination parser even though it is not reserved on the source inventory.", identifier)
	}
	return reservedIdentifierIssue{
		Database:        databaseName,
		ObjectType:      objectType,
		ObjectName:      objectName,
		Identifier:      identifier,
		Reason:          reason,
		DestinationOnly: !sourceReserved,
		Proposal:        fmt.Sprintf("Rename %s %s or keep every reference permanently quoted before cutover; do not rely on destination parser compatibility for %q.", objectType, qualifyObjectName(databaseName, objectName), identifier),
	}, true
}

func detectCaseCollisionIssues(databases []string, objects []inventoryObject) []caseCollisionIssue {
	issues := make([]caseCollisionIssue, 0, 4)
	databaseGroups := map[string][]string{}
	for _, databaseName := range databases {
		folded := strings.ToLower(databaseName)
		databaseGroups[folded] = append(databaseGroups[folded], databaseName)
	}
	for folded, names := range databaseGroups {
		if len(names) < 2 {
			continue
		}
		sort.Strings(names)
		issues = append(issues, caseCollisionIssue{
			ObjectType: "database",
			FoldedName: folded,
			Objects:    names,
			Proposal:   fmt.Sprintf("Rename databases %s so their folded names stop colliding before moving across lower_case_table_names boundaries.", strings.Join(names, ", ")),
		})
	}

	relationGroups := map[string][]string{}
	for _, object := range objects {
		if object.ObjectType == "database" {
			continue
		}
		key := object.Database + "\x00" + strings.ToLower(object.ObjectName)
		relationGroups[key] = append(relationGroups[key], object.ObjectName)
	}
	for key, names := range relationGroups {
		if len(names) < 2 {
			continue
		}
		sort.Strings(names)
		parts := strings.SplitN(key, "\x00", 2)
		issues = append(issues, caseCollisionIssue{
			Database:   parts[0],
			ObjectType: "table/view",
			FoldedName: parts[1],
			Objects:    names,
			Proposal:   fmt.Sprintf("Rename objects %s in database %s so case-folding does not merge them on the destination.", strings.Join(names, ", "), parts[0]),
		})
	}

	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Database != issues[j].Database {
			return issues[i].Database < issues[j].Database
		}
		if issues[i].ObjectType != issues[j].ObjectType {
			return issues[i].ObjectType < issues[j].ObjectType
		}
		return issues[i].FoldedName < issues[j].FoldedName
	})
	return issues
}

func detectMixedCaseIdentifierIssues(databases []string, objects []inventoryObject, sourceKnown bool, sourceValue int, destKnown bool, destValue int) []mixedCaseIdentifierIssue {
	issues := make([]mixedCaseIdentifierIssue, 0, 8)
	for _, databaseName := range databases {
		if hasMixedCase(databaseName) {
			issues = append(issues, mixedCaseIdentifierIssue{
				ObjectType: "database",
				ObjectName: databaseName,
				Proposal:   fmt.Sprintf("Normalize database name %s to a single case policy before moving across lower_case_table_names-sensitive environments.", databaseName),
			})
		}
	}
	for _, object := range objects {
		if object.ObjectType == "database" || !hasMixedCase(object.ObjectName) {
			continue
		}
		issues = append(issues, mixedCaseIdentifierIssue{
			Database:   object.Database,
			ObjectType: object.ObjectType,
			ObjectName: object.ObjectName,
			Proposal:   fmt.Sprintf("Normalize %s %s to a single-case naming policy before moving it across lower_case_table_names-sensitive environments.", object.ObjectType, qualifyObjectName(object.Database, object.ObjectName)),
		})
	}
	if !(sourceKnown || destKnown) {
		return issues
	}
	if len(issues) == 0 {
		return issues
	}
	if (sourceKnown && sourceValue == 0) && (destKnown && destValue == 0) && sourceValue == destValue {
		return issues
	}
	return issues
}

func detectViewParserRiskIssues(views []viewDefinition, sourceSQLMode string, destSQLMode string) []viewParserRiskIssue {
	issues := make([]viewParserRiskIssue, 0, 4)
	if strings.TrimSpace(sourceSQLMode) == "" || strings.TrimSpace(destSQLMode) == "" {
		return issues
	}

	ansiDiff := sqlModeContains(sourceSQLMode, "ANSI_QUOTES") != sqlModeContains(destSQLMode, "ANSI_QUOTES")
	pipesDiff := sqlModeContains(sourceSQLMode, "PIPES_AS_CONCAT") != sqlModeContains(destSQLMode, "PIPES_AS_CONCAT")
	backslashDiff := sqlModeContains(sourceSQLMode, "NO_BACKSLASH_ESCAPES") != sqlModeContains(destSQLMode, "NO_BACKSLASH_ESCAPES")
	if !ansiDiff && !pipesDiff && !backslashDiff {
		return issues
	}

	for _, view := range views {
		sanitized := stripSQLCommentsAndStrings(view.Create)
		if ansiDiff && strings.Contains(sanitized, `"`) {
			issues = append(issues, viewParserRiskIssue{
				Database: view.Database,
				View:     view.View,
				Risk:     "ANSI_QUOTES differs between source and destination while the view definition still contains double quotes.",
				Proposal: fmt.Sprintf("Rewrite view %s.%s so it does not depend on ANSI_QUOTES behavior before cutover.", view.Database, view.View),
			})
		}
		if pipesDiff && strings.Contains(sanitized, "||") {
			issues = append(issues, viewParserRiskIssue{
				Database: view.Database,
				View:     view.View,
				Risk:     "PIPES_AS_CONCAT differs between source and destination while the view definition uses ||.",
				Proposal: fmt.Sprintf("Rewrite view %s.%s to use CONCAT() or align SQL mode explicitly before cutover.", view.Database, view.View),
			})
		}
		if backslashDiff && strings.Contains(sanitized, `\\`) {
			issues = append(issues, viewParserRiskIssue{
				Database: view.Database,
				View:     view.View,
				Risk:     "NO_BACKSLASH_ESCAPES differs between source and destination while the view definition still contains backslashes.",
				Proposal: fmt.Sprintf("Rewrite view %s.%s so it does not depend on backslash escape parsing before cutover.", view.Database, view.View),
			})
		}
	}

	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Database != issues[j].Database {
			return issues[i].Database < issues[j].Database
		}
		if issues[i].View != issues[j].View {
			return issues[i].View < issues[j].View
		}
		return issues[i].Risk < issues[j].Risk
	})
	return issues
}

func stripSQLCommentsAndStrings(statement string) string {
	if strings.TrimSpace(statement) == "" {
		return statement
	}
	var out strings.Builder
	out.Grow(len(statement))

	inSingle := false
	inDouble := false
	inLineComment := false
	inBlockComment := false

	for i := 0; i < len(statement); i++ {
		if inLineComment {
			if statement[i] == '\n' {
				inLineComment = false
				out.WriteByte('\n')
			}
			continue
		}
		if inBlockComment {
			if statement[i] == '*' && i+1 < len(statement) && statement[i+1] == '/' {
				i++
				inBlockComment = false
			}
			continue
		}
		if !inSingle && !inDouble {
			if statement[i] == '#' {
				inLineComment = true
				continue
			}
			if statement[i] == '-' && i+2 < len(statement) && statement[i+1] == '-' && (statement[i+2] == ' ' || statement[i+2] == '\t' || statement[i+2] == '\n') {
				i++
				inLineComment = true
				continue
			}
			if statement[i] == '/' && i+1 < len(statement) && statement[i+1] == '*' {
				i++
				inBlockComment = true
				continue
			}
		}
		if statement[i] == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if statement[i] == '"' && !inSingle {
			inDouble = !inDouble
			out.WriteByte('"')
			continue
		}
		if inSingle || inDouble {
			continue
		}
		out.WriteByte(statement[i])
	}
	return out.String()
}

func hasMixedCase(name string) bool {
	hasUpper := false
	hasLower := false
	for _, r := range name {
		if !unicode.IsLetter(r) {
			continue
		}
		if unicode.IsUpper(r) {
			hasUpper = true
		}
		if unicode.IsLower(r) {
			hasLower = true
		}
		if hasUpper && hasLower {
			return true
		}
	}
	return false
}

func qualifyObjectName(databaseName string, objectName string) string {
	if databaseName == "" || databaseName == objectName {
		return objectName
	}
	return databaseName + "." + objectName
}

func buildIdentifierPortabilityFindings(report identifierPortabilityPrecheckReport) []compat.Finding {
	findings := make([]compat.Finding, 0, len(report.Findings)+8+report.ReservedIdentifierCount+report.ViewParserRiskCount+report.CaseCollisionCount+report.MixedCaseIdentifierCount)
	if report.SourceLowerCaseKnown || report.DestLowerCaseKnown {
		findings = append(findings, compat.Finding{
			Code:     "lower_case_table_names_inventory_recorded",
			Severity: "info",
			Message:  fmt.Sprintf("Recorded lower_case_table_names inventory: source=%s destination=%s.", describeLowerCaseInventory(report.SourceLowerCaseKnown, report.SourceLowerCaseTableNames), describeLowerCaseInventory(report.DestLowerCaseKnown, report.DestLowerCaseTableNames)),
			Proposal: "Keep this evidence with the migration report so case-portability decisions are tied to real server settings, not assumptions.",
		})
	}
	if report.SourceLowerCaseKnown && report.DestLowerCaseKnown && report.SourceLowerCaseTableNames != report.DestLowerCaseTableNames {
		findings = append(findings, compat.Finding{
			Code:     "lower_case_table_names_mismatch",
			Severity: "error",
			Message:  fmt.Sprintf("Source and destination lower_case_table_names differ (source=%d destination=%d).", report.SourceLowerCaseTableNames, report.DestLowerCaseTableNames),
			Proposal: "Do not cut over mixed-case schemas across this boundary until identifiers are normalized and case-collision evidence is clean.",
		})
	}
	if report.CaseCollisionCount > 0 {
		findings = append(findings, compat.Finding{
			Code:     "case_fold_collisions_detected",
			Severity: "error",
			Message:  fmt.Sprintf("Detected %d identifier collision group(s) after case folding.", report.CaseCollisionCount),
			Proposal: "Rename the colliding databases or tables/views before any lower_case_table_names-sensitive migration lane.",
		})
		for _, issue := range report.CaseCollisions {
			findings = append(findings, compat.Finding{
				Code:     "case_fold_collision",
				Severity: "error",
				Message:  fmt.Sprintf("%s folded name %q collides across objects %s.", strings.Title(issue.ObjectType), issue.FoldedName, strings.Join(issue.Objects, ", ")),
				Proposal: issue.Proposal,
			})
		}
	}
	if report.MixedCaseIdentifierCount > 0 {
		severity := "warn"
		if (report.SourceLowerCaseKnown && report.SourceLowerCaseTableNames != 0) || (report.DestLowerCaseKnown && report.DestLowerCaseTableNames != 0) {
			severity = "error"
		}
		findings = append(findings, compat.Finding{
			Code:     "mixed_case_identifiers_detected",
			Severity: severity,
			Message:  fmt.Sprintf("Detected %d mixed-case database/table/view name(s) in selected scope.", report.MixedCaseIdentifierCount),
			Proposal: "Normalize identifiers to a single case policy before migrating across case-sensitive portability boundaries.",
		})
		for _, issue := range report.MixedCaseIdentifiers {
			findings = append(findings, compat.Finding{
				Code:     "mixed_case_identifier",
				Severity: severity,
				Message:  fmt.Sprintf("Mixed-case %s %s requires case-portability review.", issue.ObjectType, qualifyObjectName(issue.Database, issue.ObjectName)),
				Proposal: issue.Proposal,
			})
		}
	}
	if report.ReservedIdentifierCount > 0 {
		destinationOnlyCount := 0
		for _, issue := range report.ReservedIdentifiers {
			if issue.DestinationOnly {
				destinationOnlyCount++
			}
		}
		summarySeverity := "warn"
		summaryCode := "destination_reserved_identifiers_warn_only"
		summaryMessage := fmt.Sprintf("Detected %d identifier(s) that collide with the destination reserved-word set but are already reserved on the source inventory.", report.ReservedIdentifierCount)
		summaryProposal := "Keep those identifiers quoted consistently, and prefer renaming them if application SQL still references them unquoted."
		if destinationOnlyCount > 0 {
			summarySeverity = "error"
			summaryCode = "destination_reserved_identifiers_detected"
			summaryMessage = fmt.Sprintf("Detected %d identifier(s) that become newly reserved on the destination parser.", destinationOnlyCount)
			summaryProposal = "Rename or permanently quote the reported identifiers before cutover; do not assume destination parser compatibility."
		}
		findings = append(findings, compat.Finding{
			Code:     summaryCode,
			Severity: summarySeverity,
			Message:  summaryMessage,
			Proposal: summaryProposal,
		})
		for _, issue := range report.ReservedIdentifiers {
			severity := "warn"
			code := "destination_reserved_identifier_warn_only"
			if issue.DestinationOnly {
				severity = "error"
				code = "destination_reserved_identifier"
			}
			findings = append(findings, compat.Finding{
				Code:     code,
				Severity: severity,
				Message:  fmt.Sprintf("%s %s uses identifier %q that collides with the destination reserved-word set.", strings.Title(issue.ObjectType), qualifyObjectName(issue.Database, issue.ObjectName), issue.Identifier),
				Proposal: issue.Proposal,
			})
		}
	}
	if report.ViewParserRiskCount > 0 {
		findings = append(findings, compat.Finding{
			Code:     "view_parser_drift_detected",
			Severity: "error",
			Message:  fmt.Sprintf("Detected %d parser-drift risk(s) in selected view definitions.", report.ViewParserRiskCount),
			Proposal: "Rewrite parser-sensitive views or align SQL mode semantics before migrate/replicate apply.",
		})
		for _, issue := range report.ViewParserRisks {
			findings = append(findings, compat.Finding{
				Code:     "view_parser_drift_risk",
				Severity: "error",
				Message:  fmt.Sprintf("View %s.%s has parser-drift risk: %s", issue.Database, issue.View, issue.Risk),
				Proposal: issue.Proposal,
			})
		}
	}
	if report.ReservedIdentifierCount == 0 && report.ViewParserRiskCount == 0 && report.CaseCollisionCount == 0 && report.MixedCaseIdentifierCount == 0 {
		findings = append(findings, compat.Finding{
			Code:     "identifier_portability_inventory_clean",
			Severity: "info",
			Message:  "Identifier portability inventory did not reveal reserved-word drift, parser-drift risks, or lower_case_table_names conflicts in selected scope.",
			Proposal: "Keep the inventory artifact anyway. Identifier portability evidence should travel with the cutover decision.",
		})
	}
	return findings
}

func describeLowerCaseInventory(known bool, value int) string {
	if !known {
		return "unknown"
	}
	return fmt.Sprintf("%d", value)
}

func writeIdentifierPortabilityPrecheckReport(out io.Writer, cfg config.RuntimeConfig, report identifierPortabilityPrecheckReport) error {
	payload := identifierPortabilityPrecheckResult{
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
		"[migrate] status=incompatible precheck=%s source_version=%q dest_version=%q reserved_identifiers=%d parser_risks=%d case_collisions=%d mixed_case=%d findings=%d\n",
		report.Name,
		report.SourceVersion,
		report.DestVersion,
		report.ReservedIdentifierCount,
		report.ViewParserRiskCount,
		report.CaseCollisionCount,
		report.MixedCaseIdentifierCount,
		len(report.Findings),
	); err != nil {
		return err
	}
	for _, finding := range report.Findings {
		if _, err := fmt.Fprintf(out, "[migrate] precheck %s code=%s message=%s proposal=%s\n", finding.Severity, finding.Code, finding.Message, finding.Proposal); err != nil {
			return err
		}
	}
	return nil
}
