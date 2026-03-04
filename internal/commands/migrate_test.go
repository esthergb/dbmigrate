package commands

import "testing"

func TestParseMigrateOptionsDefaults(t *testing.T) {
	opts, err := parseMigrateOptions(nil)
	if err != nil {
		t.Fatalf("expected parse success: %v", err)
	}
	if opts.DestEmptyRequired != true {
		t.Fatalf("expected dest-empty-required default true, got %v", opts.DestEmptyRequired)
	}
}

func TestParseMigrateOptionsExplicit(t *testing.T) {
	opts, err := parseMigrateOptions([]string{"--schema-only", "--force", "--dest-empty-required=false"})
	if err != nil {
		t.Fatalf("expected parse success: %v", err)
	}
	if !opts.SchemaOnly {
		t.Fatal("expected schema-only=true")
	}
	if !opts.Force {
		t.Fatal("expected force=true")
	}
	if opts.DestEmptyRequired {
		t.Fatal("expected dest-empty-required=false")
	}
}

func TestHasObject(t *testing.T) {
	if !hasObject([]string{"tables", "views"}, "views") {
		t.Fatal("expected views to be included")
	}
	if hasObject([]string{"tables"}, "triggers") {
		t.Fatal("did not expect triggers to be included")
	}
}
