package commands

import "testing"

func TestParseReplicateOptionsDefaults(t *testing.T) {
	opts, err := parseReplicateOptions(nil)
	if err != nil {
		t.Fatalf("expected parse success: %v", err)
	}
	if opts.ApplyDDL != "warn" {
		t.Fatalf("expected default apply-ddl warn, got %q", opts.ApplyDDL)
	}
	if opts.ConflictPolicy != "fail" {
		t.Fatalf("expected default conflict-policy fail, got %q", opts.ConflictPolicy)
	}
	if !opts.Resume {
		t.Fatal("expected default resume=true")
	}
	if opts.StartPos != 4 {
		t.Fatalf("expected default start-pos 4, got %d", opts.StartPos)
	}
}

func TestParseReplicateOptionsExplicit(t *testing.T) {
	opts, err := parseReplicateOptions([]string{
		"--apply-ddl=ignore",
		"--conflict-policy=source-wins",
		"--resume=false",
		"--start-file=mysql-bin.000010",
		"--start-pos=987",
	})
	if err != nil {
		t.Fatalf("expected parse success: %v", err)
	}
	if opts.ApplyDDL != "ignore" {
		t.Fatalf("expected apply-ddl ignore, got %q", opts.ApplyDDL)
	}
	if opts.ConflictPolicy != "source-wins" {
		t.Fatalf("expected conflict-policy source-wins, got %q", opts.ConflictPolicy)
	}
	if opts.Resume {
		t.Fatal("expected resume=false")
	}
	if opts.StartFile != "mysql-bin.000010" {
		t.Fatalf("unexpected start-file %q", opts.StartFile)
	}
	if opts.StartPos != 987 {
		t.Fatalf("unexpected start-pos %d", opts.StartPos)
	}
}

func TestParseReplicateOptionsInvalidApplyDDL(t *testing.T) {
	_, err := parseReplicateOptions([]string{"--apply-ddl=deny"})
	if err == nil {
		t.Fatal("expected parse error for invalid apply-ddl")
	}
}

func TestParseReplicateOptionsInvalidStartPos(t *testing.T) {
	_, err := parseReplicateOptions([]string{"--start-pos=3"})
	if err == nil {
		t.Fatal("expected parse error for invalid start-pos")
	}
}

func TestParseReplicateOptionsInvalidConflictPolicy(t *testing.T) {
	_, err := parseReplicateOptions([]string{"--conflict-policy=merge"})
	if err == nil {
		t.Fatal("expected parse error for invalid conflict-policy")
	}
}
