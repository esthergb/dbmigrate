package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDestinationEnforcesZeroDateStrict(t *testing.T) {
	tests := []struct {
		name    string
		sqlMode string
		want    bool
	}{
		{
			name:    "mysql default strict nozero",
			sqlMode: "ONLY_FULL_GROUP_BY,STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION",
			want:    true,
		},
		{
			name:    "strict without nozero",
			sqlMode: "ONLY_FULL_GROUP_BY,STRICT_TRANS_TABLES,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION",
			want:    false,
		},
		{
			name:    "nozero without strict",
			sqlMode: "NO_ZERO_IN_DATE,NO_ZERO_DATE,NO_ENGINE_SUBSTITUTION",
			want:    false,
		},
		{
			name:    "empty",
			sqlMode: "",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := destinationEnforcesZeroDateStrict(tt.sqlMode)
			if got != tt.want {
				t.Fatalf("destinationEnforcesZeroDateStrict(%q)=%v want=%v", tt.sqlMode, got, tt.want)
			}
		})
	}
}

func TestTemporalDefaultContainsZeroDate(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{value: "0000-00-00", want: true},
		{value: "0000-00-00 00:00:00", want: true},
		{value: "2020-00-15", want: true},
		{value: "2020-01-00 12:00:00", want: true},
		{value: "1970-01-01", want: false},
		{value: "1970-01-01 00:00:01", want: false},
		{value: "CURRENT_TIMESTAMP", want: false},
	}
	for _, tt := range tests {
		got := temporalDefaultContainsZeroDate(tt.value)
		if got != tt.want {
			t.Fatalf("temporalDefaultContainsZeroDate(%q)=%v want=%v", tt.value, got, tt.want)
		}
	}
}

func TestAutoFixAlterDefaultSQL(t *testing.T) {
	dateSQL := autoFixAlterDefaultSQL("appdb", "events", "event_date", "date")
	if dateSQL != "ALTER TABLE `appdb`.`events` ALTER COLUMN `event_date` SET DEFAULT '1970-01-01';" {
		t.Fatalf("unexpected DATE fix SQL: %s", dateSQL)
	}

	datetimeSQL := autoFixAlterDefaultSQL("appdb", "events", "event_ts", "datetime")
	if datetimeSQL != "ALTER TABLE `appdb`.`events` ALTER COLUMN `event_ts` SET DEFAULT '1970-01-01 00:00:01';" {
		t.Fatalf("unexpected DATETIME fix SQL: %s", datetimeSQL)
	}
}

func TestBuildZeroDatePrecheckFindings(t *testing.T) {
	report := zeroDateDefaultsPrecheckReport{
		Name:               "zero-date-defaults",
		DestinationSQLMode: "STRICT_TRANS_TABLES,NO_ZERO_DATE,NO_ZERO_IN_DATE",
		FixScriptPath:      "./state/precheck-zero-date-fixes.sql",
		IssueCount:         1,
		Issues: []zeroDateDefaultIssue{
			{
				Database:       "bots",
				Table:          "comments",
				Column:         "comment_date",
				ColumnType:     "datetime",
				DefaultValue:   "0000-00-00 00:00:00",
				ProposedFixSQL: "ALTER TABLE `bots`.`comments` ALTER COLUMN `comment_date` SET DEFAULT '1970-01-01 00:00:01';",
			},
		},
	}

	findings := buildZeroDatePrecheckFindings(report)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}
	if findings[0].Code != "zero_date_defaults_incompatible" {
		t.Fatalf("unexpected summary finding code: %s", findings[0].Code)
	}
	if findings[1].Code != "zero_date_default_column" {
		t.Fatalf("unexpected issue finding code: %s", findings[1].Code)
	}
	if !strings.Contains(findings[0].Proposal, "precheck-zero-date-fixes.sql") {
		t.Fatalf("expected summary proposal to include fix script path, got %q", findings[0].Proposal)
	}
	if !strings.Contains(findings[1].Proposal, "precheck-zero-date-fixes.sql") {
		t.Fatalf("expected issue proposal to include fix script path, got %q", findings[1].Proposal)
	}
}

func TestWriteAndCleanupZeroDateFixScript(t *testing.T) {
	tmp := t.TempDir()
	stateDir := filepath.Join(tmp, "state")
	issues := []zeroDateDefaultIssue{
		{
			ProposedFixSQL: "ALTER TABLE `db`.`t2` ALTER COLUMN `c2` SET DEFAULT '1970-01-01';",
		},
		{
			ProposedFixSQL: "ALTER TABLE `db`.`t1` ALTER COLUMN `c1` SET DEFAULT '1970-01-01 00:00:01';",
		},
		{
			ProposedFixSQL: "ALTER TABLE `db`.`t1` ALTER COLUMN `c1` SET DEFAULT '1970-01-01 00:00:01';",
		},
	}

	path, err := writeZeroDateFixScript(stateDir, issues)
	if err != nil {
		t.Fatalf("writeZeroDateFixScript failed: %v", err)
	}
	if path != filepath.Join(stateDir, "precheck-zero-date-fixes.sql") {
		t.Fatalf("unexpected script path: %s", path)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fix script failed: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "dbmigrate auto-generated zero-date precheck fixes") {
		t.Fatalf("missing header in fix script: %q", content)
	}
	first := "ALTER TABLE `db`.`t1` ALTER COLUMN `c1` SET DEFAULT '1970-01-01 00:00:01';"
	second := "ALTER TABLE `db`.`t2` ALTER COLUMN `c2` SET DEFAULT '1970-01-01';"
	if !strings.Contains(content, first) || !strings.Contains(content, second) {
		t.Fatalf("missing expected SQL statements in fix script: %q", content)
	}
	if strings.Count(content, first) != 1 {
		t.Fatalf("expected deduplicated statements, got content: %q", content)
	}
	if strings.Index(content, first) > strings.Index(content, second) {
		t.Fatalf("expected sorted statements in fix script, got content: %q", content)
	}

	if err := cleanupZeroDateFixScript(stateDir); err != nil {
		t.Fatalf("cleanupZeroDateFixScript failed: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected script to be deleted, stat err=%v", err)
	}
}
