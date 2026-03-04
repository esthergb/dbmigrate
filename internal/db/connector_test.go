package db

import "testing"

func TestNormalizeDSNFromURI(t *testing.T) {
	dsn, err := NormalizeDSN("mysql://user:pass@localhost:3306/app?tls=preferred")
	if err != nil {
		t.Fatalf("expected DSN conversion success: %v", err)
	}
	if dsn == "" {
		t.Fatal("expected normalized dsn")
	}
}

func TestRedactDSNURI(t *testing.T) {
	redacted := RedactDSN("mysql://user:secret@localhost:3306/app")
	if redacted == "" {
		t.Fatal("expected redacted output")
	}
	if redacted == "mysql://user:secret@localhost:3306/app" {
		t.Fatal("expected password to be redacted")
	}
}

func TestNormalizeDSNDriverFormat(t *testing.T) {
	input := "user:pass@tcp(localhost:3306)/app?tls=preferred"
	out, err := NormalizeDSN(input)
	if err != nil {
		t.Fatalf("expected parse success for driver format: %v", err)
	}
	if out != input {
		t.Fatalf("expected DSN to remain unchanged, got %q", out)
	}
}

func TestRedactDSNDriverFormat(t *testing.T) {
	redacted := RedactDSN("user:secret@tcp(localhost:3306)/app")
	if redacted == "" {
		t.Fatal("expected redacted output")
	}
	if redacted == "user:secret@tcp(localhost:3306)/app" {
		t.Fatal("expected driver password to be redacted")
	}
}
