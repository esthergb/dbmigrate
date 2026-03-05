package compat

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var versionPattern = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// Engine identifies supported database engines.
type Engine string

const (
	EngineMySQL   Engine = "mysql"
	EngineMariaDB Engine = "mariadb"
	EngineUnknown Engine = "unknown"
)

type strictLTSMatrixEntry struct {
	Engine Engine
	Major  int
	Minor  int
	Label  string
}

type profileMatrixRange struct {
	Engine   Engine
	Major    int
	MinMinor int
	MaxMinor int
	Label    string
}

var strictLTSMatrix = []strictLTSMatrixEntry{
	{Engine: EngineMySQL, Major: 8, Minor: 0, Label: "MySQL 8.0.x"},
	{Engine: EngineMySQL, Major: 8, Minor: 4, Label: "MySQL 8.4.x"},
	{Engine: EngineMariaDB, Major: 10, Minor: 11, Label: "MariaDB 10.11.x"},
	{Engine: EngineMariaDB, Major: 11, Minor: 4, Label: "MariaDB 11.4.x"},
}

var sameMajorMatrix = []profileMatrixRange{
	{Engine: EngineMySQL, Major: 8, MinMinor: 0, MaxMinor: 4, Label: "MySQL 8.0-8.4"},
	{Engine: EngineMariaDB, Major: 10, MinMinor: 6, MaxMinor: 11, Label: "MariaDB 10.6-10.11"},
	{Engine: EngineMariaDB, Major: 11, MinMinor: 0, MaxMinor: 4, Label: "MariaDB 11.0-11.4"},
}

var adjacentMinorMatrix = []profileMatrixRange{
	{Engine: EngineMySQL, Major: 8, MinMinor: 0, MaxMinor: 4, Label: "MySQL 8.0-8.4"},
	{Engine: EngineMariaDB, Major: 10, MinMinor: 6, MaxMinor: 11, Label: "MariaDB 10.6-10.11"},
	{Engine: EngineMariaDB, Major: 11, MinMinor: 0, MaxMinor: 4, Label: "MariaDB 11.0-11.4"},
}

// DowngradeProfile controls strictness for downgrade compatibility checks.
type DowngradeProfile string

const (
	ProfileStrictLTS     DowngradeProfile = "strict-lts"
	ProfileSameMajor     DowngradeProfile = "same-major"
	ProfileAdjacentMinor DowngradeProfile = "adjacent-minor"
	ProfileMaxCompat     DowngradeProfile = "max-compat"
)

// Instance captures parsed engine/version information.
type Instance struct {
	Engine     Engine `json:"engine"`
	RawVersion string `json:"raw_version"`
	Major      int    `json:"major"`
	Minor      int    `json:"minor"`
	Patch      int    `json:"patch"`
	Parsed     bool   `json:"parsed"`
	Version    string `json:"version"`
}

// Finding describes one compatibility result with remediation guidance.
type Finding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Proposal string `json:"proposal"`
}

// Report contains compatibility results for a source/destination pair.
type Report struct {
	Compatible       bool      `json:"compatible"`
	Downgrade        bool      `json:"downgrade"`
	DowngradeProfile string    `json:"downgrade_profile"`
	Source           Instance  `json:"source"`
	Dest             Instance  `json:"dest"`
	Findings         []Finding `json:"findings"`
}

// ParseInstance converts a raw VERSION() string into an Instance.
func ParseInstance(rawVersion string) Instance {
	engine := detectEngine(rawVersion)
	instance := Instance{
		Engine:     engine,
		RawVersion: strings.TrimSpace(rawVersion),
	}

	match := versionPattern.FindStringSubmatch(rawVersion)
	if len(match) != 4 {
		instance.Parsed = false
		return instance
	}

	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	patch, _ := strconv.Atoi(match[3])
	instance.Major = major
	instance.Minor = minor
	instance.Patch = patch
	instance.Parsed = true
	instance.Version = fmt.Sprintf("%d.%d.%d", major, minor, patch)
	return instance
}

// Evaluate computes compatibility and downgrade risk findings.
func Evaluate(source Instance, dest Instance, selectedDatabases []string, downgradeProfile string) Report {
	profile := normalizeDowngradeProfile(downgradeProfile)
	report := Report{
		Compatible:       true,
		DowngradeProfile: string(profile),
		Source:           source,
		Dest:             dest,
	}
	report.Findings = append(report.Findings, Finding{
		Code:     "downgrade_profile_selected",
		Severity: "info",
		Message:  fmt.Sprintf("Downgrade profile selected: %s.", profile),
		Proposal: "Use --downgrade-profile to tune compatibility strictness for this run.",
	})

	if len(selectedDatabases) > 0 {
		report.Findings = append(report.Findings, Finding{
			Code:     "partial_database_scope",
			Severity: "info",
			Message:  fmt.Sprintf("Partial scope enabled for %d database(s).", len(selectedDatabases)),
			Proposal: "This run is allowed to process only selected databases. Keep using --databases for phased cutovers.",
		})
	}

	if !source.Parsed || !dest.Parsed || source.Engine == EngineUnknown || dest.Engine == EngineUnknown {
		report.Compatible = false
		report.Findings = append(report.Findings, Finding{
			Code:     "version_or_engine_unparseable",
			Severity: "error",
			Message:  "Unable to auto-detect engine/version compatibility from source or destination VERSION() output.",
			Proposal: "Verify server version visibility and DSN connectivity, then rerun plan. If needed, pin scope with --databases while fixing metadata access.",
		})
		return report
	}

	compare := compareVersion(source, dest)
	report.Downgrade = source.Engine == dest.Engine && compare > 0

	if source.Engine != dest.Engine {
		report.Findings = append(report.Findings, Finding{
			Code:     "cross_engine_path",
			Severity: "warn",
			Message:  fmt.Sprintf("Cross-engine path detected: %s -> %s.", source.Engine, dest.Engine),
			Proposal: "Run plan/verify on a partial database set first and review object-level incompatibility report before full cutover.",
		})
		if profile == ProfileStrictLTS {
			applyCrossEngineStrictChecks(&report)
		} else {
			applyCrossEngineRiskWarnings(&report)
		}
		if report.Compatible && !report.Downgrade {
			report.Findings = append(report.Findings, Finding{
				Code:     "no_downgrade_detected",
				Severity: "info",
				Message:  "No same-engine downgrade version direction detected for this run.",
				Proposal: "Proceed with standard validation gates (plan + migrate/verify + report).",
			})
		}
		return report
	}

	if !report.Downgrade {
		report.Findings = append(report.Findings, Finding{
			Code:     "no_downgrade_detected",
			Severity: "info",
			Message:  "No downgrade version direction detected for this run.",
			Proposal: "Proceed with standard validation gates (plan + migrate/verify + report).",
		})
		return report
	}

	report.Findings = append(report.Findings, Finding{
		Code:     "downgrade_detected",
		Severity: "warn",
		Message:  fmt.Sprintf("Downgrade detected: source %s %s -> destination %s %s.", source.Engine, source.Version, dest.Engine, dest.Version),
		Proposal: fmt.Sprintf("Policy %q applies: fail on incompatible downgrade ranges and include remediation in report output.", profile),
	})

	switch profile {
	case ProfileStrictLTS:
		applyStrictLTSSameEngineChecks(&report)
	case ProfileSameMajor:
		applySameMajorChecks(&report)
	case ProfileAdjacentMinor:
		applyAdjacentMinorChecks(&report)
	case ProfileMaxCompat:
		report.Findings = append(report.Findings, Finding{
			Code:     "max_compat_profile",
			Severity: "warn",
			Message:  "max-compat profile selected: downgrade guardrails are relaxed.",
			Proposal: "Run full verification and inspect detailed reports before cutover because compatibility checks are permissive.",
		})
	}

	if report.Compatible {
		if !report.Downgrade {
			report.Findings = append(report.Findings, Finding{
				Code:     "no_downgrade_detected",
				Severity: "info",
				Message:  "No downgrade version direction detected for this run.",
				Proposal: "Proceed with standard validation gates (plan + migrate/verify + report).",
			})
		}
		report.Findings = append(report.Findings, Finding{
			Code:     "downgrade_allowed_by_profile",
			Severity: "info",
			Message:  fmt.Sprintf("Downgrade %s %s -> %s %s is allowed by profile %q.", source.Engine, source.Version, dest.Engine, dest.Version, profile),
			Proposal: "Proceed with migrate/verify and keep detailed report artifacts for rollback decisions.",
		})
	}
	return report
}

func normalizeDowngradeProfile(value string) DowngradeProfile {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(ProfileStrictLTS):
		return ProfileStrictLTS
	case string(ProfileSameMajor):
		return ProfileSameMajor
	case string(ProfileAdjacentMinor):
		return ProfileAdjacentMinor
	case string(ProfileMaxCompat):
		return ProfileMaxCompat
	default:
		return ProfileStrictLTS
	}
}

func applyStrictLTSSameEngineChecks(report *Report) {
	source := report.Source
	dest := report.Dest
	sourceLine, sourceKnown := strictLTSLine(source)
	destLine, destKnown := strictLTSLine(dest)
	matrixSummary := strictLTSMatrixSummary()
	if !sourceKnown || !destKnown {
		report.Compatible = false
		report.Findings = append(report.Findings, Finding{
			Code:     "strict_lts_matrix_out_of_range",
			Severity: "error",
			Message:  fmt.Sprintf("strict-lts profile requires source/destination versions inside the explicit matrix, got %s %s -> %s %s.", source.Engine, source.Version, dest.Engine, dest.Version),
			Proposal: fmt.Sprintf("Allowed strict-lts same-engine ranges: %s. Use same-major/adjacent-minor/max-compat for broader compatibility.", matrixSummary),
		})
		return
	}

	if sourceLine != destLine {
		report.Compatible = false
		report.Findings = append(report.Findings, Finding{
			Code:     "strict_lts_matrix_mismatch",
			Severity: "error",
			Message:  fmt.Sprintf("strict-lts profile blocks downgrade across matrix lines: %s -> %s.", sourceLine, destLine),
			Proposal: fmt.Sprintf("Use a staged path that stays in one strict-lts line (%s) or switch profile after risk review.", matrixSummary),
		})
		return
	}

	report.Findings = append(report.Findings, Finding{
		Code:     "strict_lts_matrix_match",
		Severity: "info",
		Message:  fmt.Sprintf("strict-lts matrix line matched: %s.", sourceLine),
		Proposal: "Proceed with standard migration/verification gates under strict-lts policy.",
	})
}

func applyCrossEngineStrictChecks(report *Report) {
	source := report.Source
	dest := report.Dest
	if source.Engine == EngineMySQL && dest.Engine == EngineMariaDB && source.Major >= 8 {
		if dest.Major < 10 || (dest.Major == 10 && dest.Minor < 6) {
			report.Compatible = false
			report.Findings = append(report.Findings, Finding{
				Code:     "mysql8_to_old_mariadb_downgrade",
				Severity: "error",
				Message:  fmt.Sprintf("MySQL %s downgrade to MariaDB %s is below minimum safe cross-engine baseline.", source.Version, dest.Version),
				Proposal: "Upgrade destination MariaDB to >= 10.6 or use staged same-engine downgrade before cross-engine migration.",
			})
		}
	}

	if source.Engine == EngineMariaDB && dest.Engine == EngineMySQL && source.Major >= 11 && dest.Major <= 5 {
		report.Compatible = false
		report.Findings = append(report.Findings, Finding{
			Code:     "mariadb11_to_mysql57_downgrade",
			Severity: "error",
			Message:  fmt.Sprintf("MariaDB %s downgrade to MySQL %s is not compatible by default policy.", source.Version, dest.Version),
			Proposal: "Target MySQL 8.0+ or split migration using --databases and convert incompatible objects before retry.",
		})
	}
}

func applyCrossEngineRiskWarnings(report *Report) {
	source := report.Source
	dest := report.Dest
	if source.Engine == EngineMySQL && dest.Engine == EngineMariaDB && source.Major >= 8 {
		if dest.Major < 10 || (dest.Major == 10 && dest.Minor < 6) {
			report.Findings = append(report.Findings, Finding{
				Code:     "mysql8_to_old_mariadb_risk",
				Severity: "warn",
				Message:  fmt.Sprintf("MySQL %s to MariaDB %s is high-risk on this profile.", source.Version, dest.Version),
				Proposal: "Prefer MariaDB >= 10.6 and run full verify modes before cutover.",
			})
		}
	}

	if source.Engine == EngineMariaDB && dest.Engine == EngineMySQL && source.Major >= 11 && dest.Major <= 5 {
		report.Findings = append(report.Findings, Finding{
			Code:     "mariadb11_to_mysql57_risk",
			Severity: "warn",
			Message:  fmt.Sprintf("MariaDB %s to MySQL %s is high-risk on this profile.", source.Version, dest.Version),
			Proposal: "Prefer MySQL 8.0+ and run full verify modes before cutover.",
		})
	}
}

func strictLTSLine(instance Instance) (string, bool) {
	for _, entry := range strictLTSMatrix {
		if instance.Engine == entry.Engine && instance.Major == entry.Major && instance.Minor == entry.Minor {
			return entry.Label, true
		}
	}
	return "", false
}

func strictLTSMatrixSummary() string {
	labels := make([]string, 0, len(strictLTSMatrix))
	for _, entry := range strictLTSMatrix {
		labels = append(labels, fmt.Sprintf("%s->%s", entry.Label, entry.Label))
	}
	return strings.Join(labels, ", ")
}

func applySameMajorChecks(report *Report) {
	source := report.Source
	dest := report.Dest
	sourceRange, sourceKnown := profileMatrixRangeForInstance(source, sameMajorMatrix)
	destRange, destKnown := profileMatrixRangeForInstance(dest, sameMajorMatrix)
	matrixSummary := profileMatrixSummary(sameMajorMatrix)

	if !sourceKnown || !destKnown {
		report.Compatible = false
		report.Findings = append(report.Findings, Finding{
			Code:     "same_major_matrix_out_of_range",
			Severity: "error",
			Message:  fmt.Sprintf("same-major profile requires source/destination versions inside the explicit matrix, got %s %s -> %s %s.", source.Engine, source.Version, dest.Engine, dest.Version),
			Proposal: fmt.Sprintf("Allowed same-major ranges: %s. Use max-compat for broader compatibility after risk review.", matrixSummary),
		})
		return
	}

	if source.Major != dest.Major {
		report.Compatible = false
		report.Findings = append(report.Findings, Finding{
			Code:     "downgrade_major_mismatch",
			Severity: "error",
			Message:  fmt.Sprintf("same-major profile blocks downgrade across major versions: %d -> %d.", source.Major, dest.Major),
			Proposal: fmt.Sprintf("Target an intermediate version inside the same major (%s) or switch profile if explicitly accepted.", matrixSummary),
		})
		return
	}

	if sourceRange != destRange {
		report.Compatible = false
		report.Findings = append(report.Findings, Finding{
			Code:     "same_major_matrix_mismatch",
			Severity: "error",
			Message:  fmt.Sprintf("same-major profile blocks downgrade across explicit matrix ranges: %s -> %s.", sourceRange, destRange),
			Proposal: fmt.Sprintf("Use staged downgrades inside one matrix range (%s) or switch profile after risk review.", matrixSummary),
		})
		return
	}

	report.Findings = append(report.Findings, Finding{
		Code:     "same_major_matrix_match",
		Severity: "info",
		Message:  fmt.Sprintf("same-major matrix range matched: %s.", sourceRange),
		Proposal: "Proceed with standard migration/verification gates under same-major policy.",
	})
}

func applyAdjacentMinorChecks(report *Report) {
	source := report.Source
	dest := report.Dest
	sourceRange, sourceKnown := profileMatrixRangeForInstance(source, adjacentMinorMatrix)
	destRange, destKnown := profileMatrixRangeForInstance(dest, adjacentMinorMatrix)
	matrixSummary := profileMatrixSummary(adjacentMinorMatrix)

	if !sourceKnown || !destKnown {
		report.Compatible = false
		report.Findings = append(report.Findings, Finding{
			Code:     "adjacent_minor_matrix_out_of_range",
			Severity: "error",
			Message:  fmt.Sprintf("adjacent-minor profile requires source/destination versions inside the explicit matrix, got %s %s -> %s %s.", source.Engine, source.Version, dest.Engine, dest.Version),
			Proposal: fmt.Sprintf("Allowed adjacent-minor ranges: %s. Use same-major or max-compat outside these ranges after risk review.", matrixSummary),
		})
		return
	}

	if source.Major != dest.Major {
		report.Compatible = false
		report.Findings = append(report.Findings, Finding{
			Code:     "downgrade_major_mismatch",
			Severity: "error",
			Message:  fmt.Sprintf("adjacent-minor profile blocks downgrade across major versions: %d -> %d.", source.Major, dest.Major),
			Proposal: fmt.Sprintf("Use a same-major destination inside matrix ranges (%s) or select max-compat with explicit risk acceptance.", matrixSummary),
		})
		return
	}

	if sourceRange != destRange {
		report.Compatible = false
		report.Findings = append(report.Findings, Finding{
			Code:     "adjacent_minor_matrix_mismatch",
			Severity: "error",
			Message:  fmt.Sprintf("adjacent-minor profile blocks downgrade across explicit matrix ranges: %s -> %s.", sourceRange, destRange),
			Proposal: fmt.Sprintf("Keep source/destination inside one adjacent-minor matrix range (%s) or switch profile after risk review.", matrixSummary),
		})
		return
	}

	if source.Minor-dest.Minor > 1 {
		report.Compatible = false
		report.Findings = append(report.Findings, Finding{
			Code:     "downgrade_minor_gap",
			Severity: "error",
			Message:  fmt.Sprintf("adjacent-minor profile allows at most one minor step downgrade, got %d.%d -> %d.%d.", source.Major, source.Minor, dest.Major, dest.Minor),
			Proposal: "Use staged downgrades one minor step at a time or select max-compat after risk review.",
		})
		return
	}

	report.Findings = append(report.Findings, Finding{
		Code:     "adjacent_minor_matrix_match",
		Severity: "info",
		Message:  fmt.Sprintf("adjacent-minor matrix range matched: %s.", sourceRange),
		Proposal: "Proceed with one-minor-step downgrade gates and full verification.",
	})
}

func profileMatrixRangeForInstance(instance Instance, matrix []profileMatrixRange) (string, bool) {
	for _, entry := range matrix {
		if instance.Engine != entry.Engine || instance.Major != entry.Major {
			continue
		}
		if instance.Minor < entry.MinMinor || instance.Minor > entry.MaxMinor {
			continue
		}
		return entry.Label, true
	}
	return "", false
}

func profileMatrixSummary(matrix []profileMatrixRange) string {
	labels := make([]string, 0, len(matrix))
	for _, entry := range matrix {
		labels = append(labels, fmt.Sprintf("%s->%s", entry.Label, entry.Label))
	}
	return strings.Join(labels, ", ")
}

func detectEngine(rawVersion string) Engine {
	raw := strings.ToLower(strings.TrimSpace(rawVersion))
	if strings.Contains(raw, "mariadb") {
		return EngineMariaDB
	}
	if raw != "" {
		return EngineMySQL
	}
	return EngineUnknown
}

// compareVersion returns >0 if source > dest, 0 if equal, <0 if source < dest.
func compareVersion(source Instance, dest Instance) int {
	if source.Major != dest.Major {
		return source.Major - dest.Major
	}
	if source.Minor != dest.Minor {
		return source.Minor - dest.Minor
	}
	return source.Patch - dest.Patch
}
