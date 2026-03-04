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
	Compatible bool      `json:"compatible"`
	Downgrade  bool      `json:"downgrade"`
	Source     Instance  `json:"source"`
	Dest       Instance  `json:"dest"`
	Findings   []Finding `json:"findings"`
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
func Evaluate(source Instance, dest Instance, selectedDatabases []string) Report {
	report := Report{
		Compatible: true,
		Source:     source,
		Dest:       dest,
	}

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
	}

	if source.Engine == dest.Engine {
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
			Proposal: "Strict policy applies: fail on any incompatible feature and provide remediation proposals in report output.",
		})

		if source.Major-dest.Major > 1 {
			report.Compatible = false
			report.Findings = append(report.Findings, Finding{
				Code:     "downgrade_major_gap",
				Severity: "error",
				Message:  fmt.Sprintf("Downgrade major version gap is too large for safe same-engine migration: %d -> %d.", source.Major, dest.Major),
				Proposal: "Use an intermediate destination version bridge or perform phased per-database migration using --databases.",
			})
		}
		return report
	}

	// Cross-engine checks: keep maximum compatibility unless a known hard-risk threshold is met.
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
