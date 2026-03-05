package commands

import "testing"

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
	if findings[1].Proposal == "" {
		t.Fatal("expected auto-fix proposal in issue finding")
	}
}
