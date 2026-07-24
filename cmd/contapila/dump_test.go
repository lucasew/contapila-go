package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDumpUnknownDialect(t *testing.T) {
	_, _, err := runCLI(t, "dump", "nope-v1", "x.pdf")
	if err == nil {
		t.Fatal("expected error for unknown dump subcommand")
	}
	if !strings.Contains(err.Error(), `unknown command "nope-v1"`) {
		t.Fatalf("err=%v", err)
	}
}

func TestDumpMissingDialect(t *testing.T) {
	_, _, err := runCLI(t, "dump")
	if err == nil {
		t.Fatal("expected error for bare dump")
	}
	if !strings.Contains(err.Error(), "missing dialect") {
		t.Fatalf("err=%v", err)
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

func TestDumpXLSXFixture(t *testing.T) {
	path := filepath.Join("..", "..", "internal", "dump", "xlsxexcelizev1", "testdata", "sample.xlsx")
	stdout, _, err := runCLI(t, "dump", "xlsx-excelize-v1", path)
	if err != nil {
		t.Fatalf("dump: %v", err)
	}
	if !strings.Contains(stdout, `"dialect":"xlsx-excelize-v1"`) {
		t.Fatalf("stdout: %s", stdout)
	}
	if !strings.Contains(stdout, `"type":"workbook"`) {
		t.Fatalf("missing workbook node: %s", stdout)
	}
}

func TestDumpPasswordFlagAccepted(t *testing.T) {
	// Unencrypted fixture must still open when --password is set (wrong/extra password ignored if not encrypted).
	path := filepath.Join("..", "..", "internal", "dump", "pdfdslipakv1", "testdata", "sample.pdf")
	stdout, _, err := runCLI(t, "dump", "--password", "unused", "pdf-dslipak-v1", path)
	if err != nil {
		t.Fatalf("dump with --password on plain pdf: %v", err)
	}
	if !strings.Contains(stdout, `"dialect":"pdf-dslipak-v1"`) {
		t.Fatalf("stdout: %s", stdout)
	}
}
