package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDumpUnknownDialect(t *testing.T) {
	_, _, err := runCLI(t, "dump", "nope-v1", "x.pdf")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "unknown dialect") {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(msg, "pdf-dslipak-v1") {
		t.Fatalf("expected dialect list in error, got %q", msg)
	}
}

func TestDumpPDFFixture(t *testing.T) {
	path := filepath.Join("..", "..", "internal", "dump", "pdfdslipakv1", "testdata", "sample.pdf")
	stdout, _, err := runCLI(t, "dump", "pdf-dslipak-v1", path)
	if err != nil {
		t.Fatalf("dump: %v", err)
	}
	if !strings.Contains(stdout, `"dialect":"pdf-dslipak-v1"`) {
		t.Fatalf("stdout: %s", stdout)
	}
	if !strings.Contains(stdout, `"type":"document"`) {
		t.Fatalf("missing document node: %s", stdout)
	}
}
