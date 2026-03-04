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
	if opts.ChunkSize != 1000 {
		t.Fatalf("expected default chunk size 1000, got %d", opts.ChunkSize)
	}
}

func TestParseMigrateOptionsExplicit(t *testing.T) {
	opts, err := parseMigrateOptions([]string{"--schema-only", "--force", "--dest-empty-required=false", "--chunk-size=250", "--resume"})
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
	if opts.ChunkSize != 250 {
		t.Fatalf("expected chunk size 250, got %d", opts.ChunkSize)
	}
	if !opts.Resume {
		t.Fatal("expected resume=true")
	}
}

func TestParseMigrateOptionsInvalidChunk(t *testing.T) {
	_, err := parseMigrateOptions([]string{"--chunk-size=0"})
	if err == nil {
		t.Fatal("expected parse error for invalid chunk-size")
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
