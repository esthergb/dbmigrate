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
		if source.Major != dest.Major {
			report.Compatible = false
			report.Findings = append(report.Findings, Finding{
				Code:     "downgrade_major_mismatch",
				Severity: "error",
				Message:  fmt.Sprintf("same-major profile blocks downgrade across major versions: %d -> %d.", source.Major, dest.Major),
				Proposal: "Target an intermediate version inside the same major or switch profile if explicitly accepted.",
			})
		}
	case ProfileAdjacentMinor:
		if source.Major != dest.Major {
			report.Compatible = false
			report.Findings = append(report.Findings, Finding{
				Code:     "downgrade_major_mismatch",
				Severity: "error",
				Message:  fmt.Sprintf("adjacent-minor profile blocks downgrade across major versions: %d -> %d.", source.Major, dest.Major),
				Proposal: "Use a same-major destination or select max-compat with explicit risk acceptance.",
			})
			return report
		}
		if source.Minor-dest.Minor > 1 {
			report.Compatible = false
			report.Findings = append(report.Findings, Finding{
				Code:     "downgrade_minor_gap",
				Severity: "error",
				Message:  fmt.Sprintf("adjacent-minor profile allows at most one minor step downgrade, got %d.%d -> %d.%d.", source.Major, source.Minor, dest.Major, dest.Minor),
				Proposal: "Use staged downgrades one minor step at a time or select max-compat after risk review.",
			})
		}
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
	switch source.Engine {
	case EngineMySQL:
		sourceLine, sourceKnown := mysqlLTSLine(source)
		destLine, destKnown := mysqlLTSLine(dest)
		if !sourceKnown || !destKnown {
			report.Compatible = false
			report.Findings = append(report.Findings, Finding{
				Code:     "strict_lts_line_unknown",
				Severity: "error",
				Message:  fmt.Sprintf("strict-lts profile requires known LTS lines, got MySQL %s -> %s.", source.Version, dest.Version),
				Proposal: "Use same-major/adjacent-minor/max-compat profile if this downgrade path is intentionally outside known LTS lines.",
			})
			return
		}
		if sourceLine != destLine {
			report.Compatible = false
			report.Findings = append(report.Findings, Finding{
				Code:     "strict_lts_line_mismatch",
				Severity: "error",
				Message:  fmt.Sprintf("strict-lts profile blocks downgrade across LTS lines: MySQL %s -> %s.", sourceLine, destLine),
				Proposal: "Use a staged path that stays inside one LTS line or select a less strict profile after risk review.",
			})
		}
	case EngineMariaDB:
		sourceLine, sourceKnown := mariaDBLTSLine(source)
		destLine, destKnown := mariaDBLTSLine(dest)
		if !sourceKnown || !destKnown {
			report.Compatible = false
			report.Findings = append(report.Findings, Finding{
				Code:     "strict_lts_line_unknown",
				Severity: "error",
				Message:  fmt.Sprintf("strict-lts profile requires known LTS lines, got MariaDB %s -> %s.", source.Version, dest.Version),
				Proposal: "Use same-major/adjacent-minor/max-compat profile if this downgrade path is intentionally outside known LTS lines.",
			})
			return
		}
		if sourceLine != destLine {
			report.Compatible = false
			report.Findings = append(report.Findings, Finding{
				Code:     "strict_lts_line_mismatch",
				Severity: "error",
				Message:  fmt.Sprintf("strict-lts profile blocks downgrade across LTS lines: MariaDB %s -> %s.", sourceLine, destLine),
				Proposal: "Use a staged path that stays inside one LTS line or select a less strict profile after risk review.",
			})
		}
	}
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

func mysqlLTSLine(instance Instance) (string, bool) {
	if instance.Major != 8 {
		return "", false
	}
	if instance.Minor == 0 || instance.Minor == 4 {
		return fmt.Sprintf("%d.%d", instance.Major, instance.Minor), true
	}
	return "", false
}

func mariaDBLTSLine(instance Instance) (string, bool) {
	if instance.Major == 10 && instance.Minor == 11 {
		return "10.11", true
	}
	if instance.Major == 11 && instance.Minor == 4 {
		return "11.4", true
	}
	return "", false
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
