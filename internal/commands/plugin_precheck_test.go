package commands

import (
	"strings"
	"testing"
)

func TestDetectUnsupportedAuthPlugins(t *testing.T) {
	source := []sourceAccountPlugin{
		{User: "app_user", Host: "%", Plugin: "mysql_native_password"},
		{User: "mysql.session", Host: "localhost", Plugin: "mysql_native_password"},
		{User: "reporting", Host: "%", Plugin: "caching_sha2_password"},
	}
	dest := map[string]struct{}{
		"caching_sha2_password": {},
		"sha256_password":       {},
	}

	issues := detectUnsupportedAuthPlugins(source, dest)
	if len(issues) != 2 {
		t.Fatalf("expected 2 auth plugin issues, got %d", len(issues))
	}
	if issues[0].User != "app_user" || issues[0].AccountClass != "user-managed" {
		t.Fatalf("unexpected first auth issue: %#v", issues[0])
	}
	if issues[1].User != "mysql.session" || issues[1].AccountClass != "system" {
		t.Fatalf("unexpected second auth issue: %#v", issues[1])
	}
	if !strings.Contains(issues[0].Proposal, "mysql_native_password") {
		t.Fatalf("expected mysql_native_password-specific proposal, got %q", issues[0].Proposal)
	}
}

func TestClassifyAccount(t *testing.T) {
	if got := classifyAccount("mysql.session"); got != "system" {
		t.Fatalf("expected system class, got %q", got)
	}
	if got := classifyAccount("root"); got != "administrative" {
		t.Fatalf("expected administrative class, got %q", got)
	}
	if got := classifyAccount("app_user"); got != "user-managed" {
		t.Fatalf("expected user-managed class, got %q", got)
	}
}

func TestDetectUnsupportedStorageEngines(t *testing.T) {
	source := []sourceTableEngine{
		{Database: "app", Table: "audit_log", Engine: "aria"},
		{Database: "app", Table: "orders", Engine: "innodb"},
		{Database: "phase60", Table: "remote_feed", Engine: "connect"},
	}
	dest := map[string]string{
		"innodb": "DEFAULT",
		"myisam": "YES",
	}

	issues := detectUnsupportedStorageEngines(source, dest)
	if len(issues) != 2 {
		t.Fatalf("expected 2 storage engine issues, got %d", len(issues))
	}
	if issues[0].Database != "app" || issues[0].Table != "audit_log" || issues[0].DestinationSupport != "MISSING" {
		t.Fatalf("unexpected first engine issue: %#v", issues[0])
	}
	if issues[1].Engine != "connect" {
		t.Fatalf("unexpected second engine issue: %#v", issues[1])
	}
	if !strings.Contains(issues[1].Proposal, "plugin-backed external tables") {
		t.Fatalf("expected plugin-backed table proposal, got %q", issues[1].Proposal)
	}
}

func TestBuildStorageEngineFindingsWithEngineSubstitutionDisabled(t *testing.T) {
	findings := buildStorageEngineFindings([]storageEngineIssue{
		{
			Database:           "app",
			Table:              "audit_log",
			Engine:             "aria",
			DestinationSupport: "MISSING",
			Proposal:           "convert it",
		},
	}, false)

	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}
	if findings[0].Code != "unsupported_storage_engines_detected" {
		t.Fatalf("unexpected summary finding: %#v", findings[0])
	}
	if !strings.Contains(findings[0].Proposal, "NO_ENGINE_SUBSTITUTION") {
		t.Fatalf("expected NO_ENGINE_SUBSTITUTION hint, got %q", findings[0].Proposal)
	}
}

func TestBuildAuthPluginFindings(t *testing.T) {
	findings := buildAuthPluginFindings([]accountPluginIssue{
		{
			User:         "app_user",
			Host:         "%",
			Plugin:       "mysql_native_password",
			AccountClass: "user-managed",
			Proposal:     "rewrite account",
		},
	})

	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}
	if findings[0].Code != "unsupported_auth_plugins_detected" {
		t.Fatalf("unexpected summary finding: %#v", findings[0])
	}
	if findings[1].Code != "unsupported_auth_plugin_account" {
		t.Fatalf("unexpected detail finding: %#v", findings[1])
	}
	if !strings.Contains(findings[1].Message, "app_user") {
		t.Fatalf("expected account identity in message, got %q", findings[1].Message)
	}
}

func TestPluginStatusEnabled(t *testing.T) {
	if !pluginStatusEnabled("ACTIVE") {
		t.Fatal("expected ACTIVE to be enabled")
	}
	if !pluginStatusEnabled("enabled") {
		t.Fatal("expected enabled to be enabled")
	}
	if pluginStatusEnabled("DISABLED") {
		t.Fatal("did not expect DISABLED to be enabled")
	}
}

func TestAuthPluginProposalEd25519(t *testing.T) {
	proposal := authPluginProposal("ed25519")
	if !strings.Contains(proposal, "not available on MySQL") {
		t.Fatalf("expected ed25519-specific proposal, got %q", proposal)
	}
	proposal2 := authPluginProposal("client_ed25519")
	if !strings.Contains(proposal2, "not available on MySQL") {
		t.Fatalf("expected client_ed25519-specific proposal, got %q", proposal2)
	}
}

func TestPersistAndLoadPluginLifecyclePrecheckArtifact(t *testing.T) {
	tmp := t.TempDir()
	report := pluginLifecyclePrecheckReport{
		Name:         "plugin-lifecycle",
		Incompatible: true,
		UnsupportedAuthPlugins: []accountPluginIssue{{
			User:   "app_user",
			Host:   "%",
			Plugin: "mysql_native_password",
		}},
		UnsupportedStorageEngines: []storageEngineIssue{{
			Database: "app",
			Table:    "audit_log",
			Engine:   "aria",
		}},
	}

	if err := persistPluginLifecyclePrecheckArtifact(tmp, report); err != nil {
		t.Fatalf("persist plugin lifecycle artifact: %v", err)
	}
	loaded, err := loadPluginLifecyclePrecheckArtifact(tmp)
	if err != nil {
		t.Fatalf("load plugin lifecycle artifact: %v", err)
	}
	if !loaded.Incompatible {
		t.Fatal("expected loaded artifact to be incompatible")
	}
	if len(loaded.UnsupportedAuthPlugins) != 1 {
		t.Fatalf("expected 1 unsupported auth plugin, got %d", len(loaded.UnsupportedAuthPlugins))
	}
	if len(loaded.UnsupportedStorageEngines) != 1 {
		t.Fatalf("expected 1 unsupported engine, got %d", len(loaded.UnsupportedStorageEngines))
	}
}

func TestEngineSupportEnabled(t *testing.T) {
	if !engineSupportEnabled("DEFAULT") {
		t.Fatal("expected DEFAULT to be enabled")
	}
	if !engineSupportEnabled("yes") {
		t.Fatal("expected yes to be enabled")
	}
	if engineSupportEnabled("NO") {
		t.Fatal("did not expect NO to be enabled")
	}
}
