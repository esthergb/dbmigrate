package commands

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/esthergb/dbmigrate/internal/compat"
	"github.com/esthergb/dbmigrate/internal/config"
	"github.com/esthergb/dbmigrate/internal/schema"
	"github.com/esthergb/dbmigrate/internal/version"
)

type accountPluginIssue struct {
	User         string `json:"user"`
	Host         string `json:"host"`
	Plugin       string `json:"plugin"`
	AccountClass string `json:"account_class"`
	Proposal     string `json:"proposal"`
}

type storageEngineIssue struct {
	Database           string `json:"database"`
	Table              string `json:"table"`
	Engine             string `json:"engine"`
	DestinationSupport string `json:"destination_support"`
	Proposal           string `json:"proposal"`
}

type pluginLifecyclePrecheckReport struct {
	Name                                       string               `json:"name"`
	Incompatible                               bool                 `json:"incompatible"`
	SourceAccountCount                         int                  `json:"source_account_count"`
	SourceTableCount                           int                  `json:"source_table_count"`
	DestinationPluginCount                     int                  `json:"destination_plugin_count"`
	DestinationEngineCount                     int                  `json:"destination_engine_count"`
	DestinationSQLMode                         string               `json:"destination_sql_mode,omitempty"`
	NoEngineSubstitution                       bool                 `json:"no_engine_substitution"`
	DefaultAuthenticationPluginVariablePresent bool                 `json:"default_authentication_plugin_variable_present"`
	DestinationDefaultAuthenticationPlugin     string               `json:"destination_default_authentication_plugin,omitempty"`
	UnsupportedAuthPlugins                     []accountPluginIssue `json:"unsupported_auth_plugins,omitempty"`
	UnsupportedStorageEngines                  []storageEngineIssue `json:"unsupported_storage_engines,omitempty"`
	Findings                                   []compat.Finding     `json:"findings,omitempty"`
}

type pluginLifecyclePrecheckResult struct {
	Command   string                        `json:"command"`
	Status    string                        `json:"status"`
	Precheck  pluginLifecyclePrecheckReport `json:"precheck"`
	Timestamp time.Time                     `json:"timestamp"`
	Version   string                        `json:"version"`
}

type sourceAccountPlugin struct {
	User   string
	Host   string
	Plugin string
}

type sourceTableEngine struct {
	Database string
	Table    string
	Engine   string
}

func runPluginLifecyclePrecheck(
	ctx context.Context,
	source *sql.DB,
	dest *sql.DB,
	includeDatabases []string,
	excludeDatabases []string,
) (pluginLifecyclePrecheckReport, error) {
	report := pluginLifecyclePrecheckReport{
		Name: "plugin-lifecycle",
	}

	sqlMode, err := querySQLMode(ctx, dest)
	if err != nil {
		report.Findings = append(report.Findings, compat.Finding{
			Code:     "destination_sql_mode_inventory_unavailable",
			Severity: "warn",
			Message:  fmt.Sprintf("Unable to read destination sql_mode during plugin/engine precheck: %v.", err),
			Proposal: "Review destination sql_mode manually, especially NO_ENGINE_SUBSTITUTION, before claiming engine compatibility.",
		})
	} else {
		report.DestinationSQLMode = sqlMode
		report.NoEngineSubstitution = sqlModeContains(sqlMode, "NO_ENGINE_SUBSTITUTION")
		if !report.NoEngineSubstitution {
			report.Findings = append(report.Findings, compat.Finding{
				Code:     "destination_no_engine_substitution_disabled",
				Severity: "warn",
				Message:  "Destination sql_mode does not include NO_ENGINE_SUBSTITUTION; unsupported source engines may silently map to a different engine.",
				Proposal: "Enable NO_ENGINE_SUBSTITUTION before migration and keep failing on unsupported source engines instead of relying on engine fallback.",
			})
		}
	}

	defaultAuthPlugin, defaultAuthPresent, defaultAuthErr := queryDefaultAuthenticationPlugin(ctx, dest)
	report.DefaultAuthenticationPluginVariablePresent = defaultAuthPresent
	report.DestinationDefaultAuthenticationPlugin = defaultAuthPlugin
	if defaultAuthErr != nil {
		report.Findings = append(report.Findings, compat.Finding{
			Code:     "destination_default_auth_plugin_variable_unavailable",
			Severity: "warn",
			Message:  fmt.Sprintf("Destination default_authentication_plugin variable is unavailable: %v.", defaultAuthErr),
			Proposal: "Do not rely on default_authentication_plugin introspection alone. Inventory INFORMATION_SCHEMA.PLUGINS and normalize account plugins explicitly.",
		})
	}

	sourceAccounts, sourceAccountErr := querySourceAccountPlugins(ctx, source)
	if sourceAccountErr != nil {
		report.Findings = append(report.Findings, compat.Finding{
			Code:     "source_auth_plugin_inventory_unavailable",
			Severity: "warn",
			Message:  fmt.Sprintf("Unable to inventory source account plugins from mysql.user: %v.", sourceAccountErr),
			Proposal: "Grant metadata access for account inventory or review source users manually before any user/grant migration step.",
		})
	} else {
		report.SourceAccountCount = len(sourceAccounts)
	}

	destPlugins, destPluginErr := queryDestinationPlugins(ctx, dest)
	if destPluginErr != nil {
		report.Findings = append(report.Findings, compat.Finding{
			Code:     "destination_plugin_inventory_unavailable",
			Severity: "warn",
			Message:  fmt.Sprintf("Unable to inventory destination plugins from INFORMATION_SCHEMA.PLUGINS: %v.", destPluginErr),
			Proposal: "Review plugin availability manually before migrating users, grants, or plugin-backed objects.",
		})
	} else {
		report.DestinationPluginCount = len(destPlugins)
	}

	if sourceAccountErr == nil && destPluginErr == nil {
		report.UnsupportedAuthPlugins = detectUnsupportedAuthPlugins(sourceAccounts, destPlugins)
		report.Findings = append(report.Findings, buildAuthPluginFindings(report.UnsupportedAuthPlugins)...)
	}

	allDatabases, err := listDatabases(ctx, source)
	if err != nil {
		return report, fmt.Errorf("list source databases: %w", err)
	}
	selectedDatabases := schema.SelectDatabases(allDatabases, includeDatabases, excludeDatabases)
	if len(selectedDatabases) == 0 {
		report.Findings = append(report.Findings, compat.Finding{
			Code:     "plugin_lifecycle_no_selected_databases",
			Severity: "info",
			Message:  "No source databases selected for engine inventory.",
			Proposal: "Set --databases or verify include/exclude filters if engine compatibility should be checked for a partial migration scope.",
		})
		return report, nil
	}

	sourceTables, err := querySourceTableEngines(ctx, source, selectedDatabases)
	if err != nil {
		return report, fmt.Errorf("query source table engines: %w", err)
	}
	report.SourceTableCount = len(sourceTables)

	destEngines, err := queryDestinationEngines(ctx, dest)
	if err != nil {
		return report, fmt.Errorf("query destination engines: %w", err)
	}
	report.DestinationEngineCount = len(destEngines)

	report.UnsupportedStorageEngines = detectUnsupportedStorageEngines(sourceTables, destEngines)
	if len(report.UnsupportedStorageEngines) > 0 {
		report.Incompatible = true
	}
	report.Findings = append(report.Findings, buildStorageEngineFindings(report.UnsupportedStorageEngines, report.NoEngineSubstitution)...)

	if len(report.UnsupportedAuthPlugins) == 0 && len(report.UnsupportedStorageEngines) == 0 {
		report.Findings = append(report.Findings, compat.Finding{
			Code:     "plugin_lifecycle_inventory_clean",
			Severity: "info",
			Message:  "Auth plugin inventory and selected table-engine inventory did not reveal unsupported destination dependencies.",
			Proposal: "Keep the rehearsal artifacts anyway. Plugin and engine inventories are evidence, not a reason to skip verification.",
		})
	}

	return report, nil
}

func queryDefaultAuthenticationPlugin(ctx context.Context, db *sql.DB) (string, bool, error) {
	var value sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT @@default_authentication_plugin").Scan(&value); err != nil {
		return "", false, err
	}
	return strings.TrimSpace(value.String), true, nil
}

func querySourceAccountPlugins(ctx context.Context, db *sql.DB) ([]sourceAccountPlugin, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT User, Host, plugin
		FROM mysql.user
		WHERE COALESCE(plugin, '') <> ''
		ORDER BY User, Host
	`)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]sourceAccountPlugin, 0, 16)
	for rows.Next() {
		var item sourceAccountPlugin
		if err := rows.Scan(&item.User, &item.Host, &item.Plugin); err != nil {
			return nil, err
		}
		item.Plugin = normalizeInventoryToken(item.Plugin)
		if item.Plugin == "" {
			continue
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func queryDestinationPlugins(ctx context.Context, db *sql.DB) (map[string]struct{}, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT PLUGIN_NAME, PLUGIN_STATUS
		FROM information_schema.PLUGINS
		WHERE COALESCE(PLUGIN_NAME, '') <> ''
		ORDER BY PLUGIN_NAME
	`)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	out := map[string]struct{}{}
	for rows.Next() {
		var name string
		var status sql.NullString
		if err := rows.Scan(&name, &status); err != nil {
			return nil, err
		}
		if !pluginStatusEnabled(status.String) {
			continue
		}
		out[normalizeInventoryToken(name)] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func querySourceTableEngines(ctx context.Context, db *sql.DB, databases []string) ([]sourceTableEngine, error) {
	placeholders := make([]string, 0, len(databases))
	args := make([]any, 0, len(databases))
	for _, databaseName := range databases {
		placeholders = append(placeholders, "?")
		args = append(args, databaseName)
	}

	query := fmt.Sprintf(`
		SELECT TABLE_SCHEMA, TABLE_NAME, ENGINE
		FROM information_schema.TABLES
		WHERE TABLE_TYPE = 'BASE TABLE'
		  AND TABLE_SCHEMA IN (%s)
		ORDER BY TABLE_SCHEMA, TABLE_NAME
	`, strings.Join(placeholders, ","))

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]sourceTableEngine, 0, 32)
	for rows.Next() {
		var item sourceTableEngine
		var engine sql.NullString
		if err := rows.Scan(&item.Database, &item.Table, &engine); err != nil {
			return nil, err
		}
		item.Engine = normalizeInventoryToken(engine.String)
		if item.Engine == "" {
			continue
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func queryDestinationEngines(ctx context.Context, db *sql.DB) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT ENGINE, SUPPORT
		FROM information_schema.ENGINES
		WHERE COALESCE(ENGINE, '') <> ''
		ORDER BY ENGINE
	`)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	out := map[string]string{}
	for rows.Next() {
		var engine string
		var support sql.NullString
		if err := rows.Scan(&engine, &support); err != nil {
			return nil, err
		}
		out[normalizeInventoryToken(engine)] = strings.ToUpper(strings.TrimSpace(support.String))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func normalizeInventoryToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func pluginStatusEnabled(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "ACTIVE", "ENABLED":
		return true
	default:
		return false
	}
}

func engineSupportEnabled(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "YES", "DEFAULT":
		return true
	default:
		return false
	}
}

func detectUnsupportedAuthPlugins(source []sourceAccountPlugin, destPlugins map[string]struct{}) []accountPluginIssue {
	if len(source) == 0 || len(destPlugins) == 0 {
		return nil
	}

	issues := make([]accountPluginIssue, 0, 8)
	for _, item := range source {
		if _, ok := destPlugins[item.Plugin]; ok {
			continue
		}
		issues = append(issues, accountPluginIssue{
			User:         item.User,
			Host:         item.Host,
			Plugin:       item.Plugin,
			AccountClass: classifyAccount(item.User),
			Proposal:     authPluginProposal(item.Plugin),
		})
	}

	sort.Slice(issues, func(i, j int) bool {
		if issues[i].User == issues[j].User {
			if issues[i].Host == issues[j].Host {
				return issues[i].Plugin < issues[j].Plugin
			}
			return issues[i].Host < issues[j].Host
		}
		return issues[i].User < issues[j].User
	})
	return issues
}

func detectUnsupportedStorageEngines(sourceTables []sourceTableEngine, destEngines map[string]string) []storageEngineIssue {
	if len(sourceTables) == 0 || len(destEngines) == 0 {
		return nil
	}

	issues := make([]storageEngineIssue, 0, 8)
	for _, item := range sourceTables {
		support, ok := destEngines[item.Engine]
		if ok && engineSupportEnabled(support) {
			continue
		}
		if !ok {
			support = "MISSING"
		}
		issues = append(issues, storageEngineIssue{
			Database:           item.Database,
			Table:              item.Table,
			Engine:             item.Engine,
			DestinationSupport: support,
			Proposal:           storageEngineProposal(item.Engine),
		})
	}

	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Database == issues[j].Database {
			return issues[i].Table < issues[j].Table
		}
		return issues[i].Database < issues[j].Database
	})
	return issues
}

func classifyAccount(user string) string {
	switch strings.ToLower(strings.TrimSpace(user)) {
	case "mysql.session", "mysql.sys", "mysql.infoschema", "mariadb.sys", "mysqlxsys":
		return "system"
	case "root", "healthcheck":
		return "administrative"
	default:
		return "user-managed"
	}
}

func authPluginProposal(plugin string) string {
	switch normalizeInventoryToken(plugin) {
	case "mysql_native_password":
		return "Rewrite accounts away from mysql_native_password before cutover or recreate them manually with a destination-supported plugin."
	case "caching_sha2_password":
		return "Confirm the destination and every client in scope supports caching_sha2_password; otherwise rewrite or reset credentials deliberately."
	case "unix_socket", "auth_socket":
		return "Do not copy socket-authenticated accounts blindly across hosts. Recreate them with an explicit destination-local authentication strategy."
	default:
		return "Normalize account plugins before cutover or recreate the affected accounts manually on the destination."
	}
}

func storageEngineProposal(engine string) string {
	switch normalizeInventoryToken(engine) {
	case "aria":
		return "Convert Aria tables to a destination-supported engine such as InnoDB before migration, or keep them out of scope for this run."
	case "federated", "connect":
		return "Treat plugin-backed external tables as unsupported for ordinary logical migration. Replace them with destination-native equivalents or exclude them."
	default:
		return fmt.Sprintf("Convert %s tables to a destination-supported engine before migration, or enable the engine explicitly if that is an approved target configuration.", engine)
	}
}

func buildAuthPluginFindings(issues []accountPluginIssue) []compat.Finding {
	if len(issues) == 0 {
		return nil
	}

	findings := make([]compat.Finding, 0, len(issues)+1)
	findings = append(findings, compat.Finding{
		Code:     "unsupported_auth_plugins_detected",
		Severity: "warn",
		Message:  fmt.Sprintf("Detected %d source account(s) using auth plugins that are not active on the destination.", len(issues)),
		Proposal: "Review the detailed account list and normalize plugins before any user/grant migration or account recreation step.",
	})
	for _, issue := range issues {
		findings = append(findings, compat.Finding{
			Code:     "unsupported_auth_plugin_account",
			Severity: "warn",
			Message:  fmt.Sprintf("Source account %q@%q (%s) uses auth plugin %q, which is not active on the destination.", issue.User, issue.Host, issue.AccountClass, issue.Plugin),
			Proposal: issue.Proposal,
		})
	}
	return findings
}

func buildStorageEngineFindings(issues []storageEngineIssue, noEngineSubstitution bool) []compat.Finding {
	if len(issues) == 0 {
		return nil
	}

	findings := make([]compat.Finding, 0, len(issues)+1)
	proposal := "Convert or exclude the affected tables before rerunning plan/migrate."
	if !noEngineSubstitution {
		proposal = "Convert or exclude the affected tables before rerunning plan/migrate, and enable NO_ENGINE_SUBSTITUTION to prevent silent engine fallback."
	}
	findings = append(findings, compat.Finding{
		Code:     "unsupported_storage_engines_detected",
		Severity: "error",
		Message:  fmt.Sprintf("Detected %d source table(s) whose storage engine is not supported on the destination.", len(issues)),
		Proposal: proposal,
	})
	for _, issue := range issues {
		findings = append(findings, compat.Finding{
			Code:     "unsupported_storage_engine_table",
			Severity: "error",
			Message:  fmt.Sprintf("Source table %s.%s uses engine %q, but destination support is %q.", issue.Database, issue.Table, issue.Engine, issue.DestinationSupport),
			Proposal: issue.Proposal,
		})
	}
	return findings
}

func writePluginLifecyclePrecheckReport(out io.Writer, cfg config.RuntimeConfig, report pluginLifecyclePrecheckReport) error {
	payload := pluginLifecyclePrecheckResult{
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
		"[migrate] status=incompatible precheck=%s no_engine_substitution=%v default_auth_plugin_variable_present=%v default_auth_plugin=%q findings=%d\n",
		report.Name,
		report.NoEngineSubstitution,
		report.DefaultAuthenticationPluginVariablePresent,
		report.DestinationDefaultAuthenticationPlugin,
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
