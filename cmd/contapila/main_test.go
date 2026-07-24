package main

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucasew/contapila-go/pkg/version"
	"github.com/spf13/cobra"
)

// exampleDir is the multi-ledger fixture used for CLI smoke tests.
func exampleDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "example"))
	if err != nil {
		t.Fatalf("abs example dir: %v", err)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("example fixture missing at %s: %v", dir, err)
	}
	return dir
}

// newTestRoot mirrors main()'s cobra tree so same-package tests can drive
// status/check/parse without calling main() (which os.Exit on failure).
func newTestRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "contapila",
		Short:         "Contapila — Beancount-class ledger in Go",
		Version:       version.GetBuildID(),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if verbose {
				logLevel.Set(slog.LevelDebug)
			}
			if workDir == "" {
				return nil
			}
			abs, err := filepath.Abs(workDir)
			if err != nil {
				return fmt.Errorf("-C %s: %w", workDir, err)
			}
			info, err := os.Stat(abs)
			if err != nil {
				return fmt.Errorf("-C %s: %w", workDir, err)
			}
			if !info.IsDir() {
				return fmt.Errorf("-C %s: not a directory", workDir)
			}
			workDir = abs
			return nil
		},
	}
	root.PersistentFlags().StringVarP(&workDir, "directory", "C", "", "run as if contapila started in this directory")
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable debug logging on stderr")
	root.AddCommand(
		statusCmd(), checkCmd(), balancesCmd(), journalCmd(), pnlCmd(),
		networthCmd(), accountCmd(), parseCmd(), ingestCmd(), webCmd(), desktopCmd(), lspCmd(), dumpCmd(),
	)
	return root
}

// runCLI executes the CLI with args, capturing stdout/stderr. Resets package
// globals bound by -C/--verbose between calls.
func runCLI(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	workDir = ""
	verbose = false
	dumpPassword = ""
	logLevel.Set(slog.LevelInfo)
	t.Cleanup(func() {
		workDir = ""
		verbose = false
		dumpPassword = ""
		logLevel.Set(slog.LevelInfo)
	})

	root := newTestRoot()
	root.SetArgs(args)

	oldOut, oldErr := os.Stdout, os.Stderr
	or, ow, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("stdout pipe: %v", pipeErr)
	}
	er, ew, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("stderr pipe: %v", pipeErr)
	}
	os.Stdout, os.Stderr = ow, ew

	// Point slog at the pipe so --verbose noise does not leak into the runner.
	slog.SetDefault(slog.New(slog.NewTextHandler(ew, &slog.HandlerOptions{Level: logLevel})))

	execErr := root.Execute()

	_ = ow.Close()
	_ = ew.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	var outBuf, errBuf bytes.Buffer
	_, _ = io.Copy(&outBuf, or)
	_, _ = io.Copy(&errBuf, er)
	_ = or.Close()
	_ = er.Close()

	return outBuf.String(), errBuf.String(), execErr
}

func TestStatusExample(t *testing.T) {
	dir := exampleDir(t)
	out, _, err := runCLI(t, "-C", dir, "status")
	if err != nil {
		t.Fatalf("status: %v\nstdout:\n%s", err, out)
	}
	for _, want := range []string{
		"Project root:",
		"Ledgers (4):",
		"personal",
		"acme",
		"CUE:               Unified OK",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status stdout missing %q\n%s", want, out)
		}
	}
}

func TestCheckExample(t *testing.T) {
	dir := exampleDir(t)
	out, errOut, err := runCLI(t, "-C", dir, "check")
	if err != nil {
		t.Fatalf("check: %v\nstdout:\n%s\nstderr:\n%s", err, out, errOut)
	}
	// One OK line per ledger in the example fixture.
	if strings.Count(out, "OK") < 4 {
		t.Errorf("check expected OK for each ledger, got:\n%s", out)
	}
	for _, name := range []string{"acme", "ong", "personal", "smuggle"} {
		if !strings.Contains(out, "== "+name+" ==") {
			t.Errorf("check missing ledger header %q\n%s", name, out)
		}
	}
}

func TestCheckExampleSingleLedger(t *testing.T) {
	dir := exampleDir(t)
	out, errOut, err := runCLI(t, "-C", dir, "check", "personal")
	if err != nil {
		t.Fatalf("check personal: %v\nstdout:\n%s\nstderr:\n%s", err, out, errOut)
	}
	if !strings.Contains(out, "== personal ==") {
		t.Errorf("missing personal header\n%s", out)
	}
	if !strings.Contains(out, "OK") {
		t.Errorf("missing OK\n%s", out)
	}
	if strings.Contains(out, "== acme ==") {
		t.Errorf("single-ledger check should not load acme\n%s", out)
	}
}

func TestParseCommodities(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("..", "..", "testdata", "example", "commodities.beancount"))
	if err != nil {
		t.Fatal(err)
	}
	out, errOut, err := runCLI(t, "parse", path)
	if err != nil {
		t.Fatalf("parse: %v\nstdout:\n%s\nstderr:\n%s", err, out, errOut)
	}
	if !strings.Contains(out, "ast.Commodity") {
		t.Errorf("parse expected commodity directives, got:\n%s", out)
	}
}

func TestDirectoryFlagMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-project-root")
	_, errOut, err := runCLI(t, "-C", missing, "status")
	if err == nil {
		t.Fatal("expected error for missing -C directory")
	}
	if !strings.Contains(err.Error(), "-C") && !strings.Contains(errOut, "-C") {
		t.Errorf("error should mention -C; err=%v stderr=%q", err, errOut)
	}
}
