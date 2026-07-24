package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/lucasew/contapila-go/internal/ast"
	"github.com/lucasew/contapila-go/internal/diag"
	"github.com/lucasew/contapila-go/internal/engine"
	"github.com/lucasew/contapila-go/internal/ingest"
	"github.com/lucasew/contapila-go/internal/lsp"
	"github.com/lucasew/contapila-go/internal/parser"
	"github.com/lucasew/contapila-go/internal/period"
	"github.com/lucasew/contapila-go/internal/web"
	"github.com/lucasew/contapila-go/pkg/project"
	"github.com/lucasew/contapila-go/pkg/version"
	"github.com/spf13/cobra"
)

// workDir is the optional start directory for project discovery (global -C).
// Empty means use the process working directory.
var workDir string

// verbose enables Debug-level slog output (--verbose). Default log level is Info.
var verbose bool

// logLevel is the live slog level (Info by default; Debug when --verbose).
var logLevel = &slog.LevelVar{} // zero value is LevelInfo

func main() {
	// Text handler on stderr so engine/project slog.Warn (and Info) stay
	// operator-visible. Level Info by default; Debug with --verbose.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))

	root := &cobra.Command{
		Use:           "contapila",
		Short:         "Contapila — Beancount-class ledger in Go",
		Version:       version.GetBuildID(),
		SilenceUsage:  true,
		SilenceErrors: true,
		// Apply --verbose / -C before subcommands; discovery starts from -C.
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
	root.PersistentFlags().StringVarP(&workDir, "directory", "C", "", "run as if contapila started in this directory (project discovery)")
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable debug logging on stderr")
	root.AddCommand(statusCmd(), checkCmd(), balancesCmd(), journalCmd(), pnlCmd(), networthCmd(), accountCmd(), parseCmd(), ingestCmd(), webCmd(), desktopCmd(), lspCmd(), dumpCmd())

	// Not-a-TTY bare launch / project path → desktop (SPEC §3.2.1).
	// After flag registration so workDir is not wiped by StringVarP defaults;
	// before Execute so "contapila /path" is not an unknown command.
	applyDesktopRewrite()

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// projectCwd returns the project search start directory: -C if set, else process CWD.
func projectCwd() (string, error) {
	if workDir != "" {
		return workDir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	return cwd, nil
}

func printDiags(ds diag.List) {
	if len(ds) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, ds.Format())
}

func withLedgers(args []string, fn func(*engine.Ledger) error) error {
	cwd, err := projectCwd()
	if err != nil {
		return err
	}
	p, pdb, pdiags, err := engine.OpenProject(cwd)
	if err != nil {
		return err
	}
	printDiags(pdiags)
	names := args
	if len(names) == 0 {
		names = engine.LedgerNames(p)
		if len(names) == 0 {
			return fmt.Errorf("zero ledgers found")
		}
	}
	var failed bool
	for _, name := range names {
		l, err := engine.OpenLedger(p, pdb, name)
		if err != nil {
			return err
		}
		if err := fn(l); err != nil {
			failed = true
			fmt.Fprintln(os.Stderr, err)
		}
	}
	if failed {
		return fmt.Errorf("one or more ledgers failed")
	}
	return nil
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use: "status", Aliases: []string{"doctor"}, Short: "Show project status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := projectCwd()
			if err != nil {
				return err
			}
			p, err := project.OpenProject(cwd)
			if err != nil {
				return err
			}
			fmt.Printf("Project root:      %s\n", p.Root)
			fmt.Printf("contapila.cue:     %s\n", filepath.Join(p.Root, "contapila.cue"))
			if len(p.Ledgers) == 0 {
				return fmt.Errorf("zero ledgers found")
			}
			fmt.Printf("Ledgers (%d):\n", len(p.Ledgers))
			for _, l := range p.Ledgers {
				fmt.Printf("  - %s (%s)\n", l.Name, l.MainPath)
			}
			if p.PricesPath != "" {
				switch {
				case p.PricesMissing:
					fmt.Printf("Prices:            %s (missing)\n", p.PricesPath)
				case p.PricesEmpty:
					fmt.Printf("Prices:            %s (empty)\n", p.PricesPath)
				default:
					fmt.Printf("Prices:            %s\n", p.PricesPath)
				}
			}
			if len(p.StreamJournals) > 0 {
				fmt.Printf("Stream journals (%d):\n", len(p.StreamJournals))
				for _, j := range p.StreamJournals {
					fmt.Printf("  - %s\n", j.Path)
				}
			}
			fmt.Println("CUE:               Unified OK")
			return nil
		},
	}
}

func checkCmd() *cobra.Command {
	return &cobra.Command{
		Use: "check [ledger]", Short: "Validate ledger(s)", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withLedgers(args, func(l *engine.Ledger) error {
				fmt.Printf("== %s ==\n", l.Name)
				printDiags(l.Diags)
				if l.Diags.HasErrors() {
					return fmt.Errorf("check failed for %s", l.Name)
				}
				fmt.Println("OK")
				return nil
			})
		},
	}
}

func balancesCmd() *cobra.Command {
	var asOf string
	c := &cobra.Command{
		Use: "balances [ledger]", Short: "Balances as-of", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := engine.ParseDate(asOf)
			if err != nil {
				return err
			}
			if t.IsZero() {
				t = engine.AsOfLatest
			}
			// Single ledger: hierarchical tree. Multi-ledger: flat sorted table.
			if len(args) == 1 {
				return withLedgers(args, func(l *engine.Ledger) error {
					tree := l.BalancesTree(t)
					fmt.Printf("== %s balances ==\n", l.Name)
					for _, ln := range tree {
						pad := strings.Repeat("  ", ln.Depth)
						mark := "  "
						if ln.IsRollup {
							mark = "Σ "
						}
						name := ln.Name
						if name == "" {
							name = ln.Account
						}
						amt := ""
						if ln.Amount != nil {
							amt = ln.Amount.FloatString(4)
						}
						fmt.Printf("%s%s%-28s %12s %s\n", pad, mark, name, amt, ln.Commodity)
					}
					return nil
				})
			}
			type row struct {
				ledger, account, amount, commodity string
			}
			var rows []row
			err = withLedgers(args, func(l *engine.Ledger) error {
				bals := l.BalancesAsOf(t)
				var accts []string
				for a := range bals {
					accts = append(accts, a)
				}
				sort.Strings(accts)
				for _, a := range accts {
					var cs []string
					for c := range bals[a] {
						cs = append(cs, c)
					}
					sort.Strings(cs)
					for _, c := range cs {
						rows = append(rows, row{
							ledger:    l.Name,
							account:   a,
							amount:    bals[a][c].FloatString(6),
							commodity: c,
						})
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
			sort.Slice(rows, func(i, j int) bool {
				if rows[i].account != rows[j].account {
					return rows[i].account < rows[j].account
				}
				if rows[i].commodity != rows[j].commodity {
					return rows[i].commodity < rows[j].commodity
				}
				return rows[i].ledger < rows[j].ledger
			})
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "LEDGER\tACCOUNT\tAMOUNT\tCOMMODITY")
			for _, r := range rows {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.ledger, r.account, r.amount, r.commodity)
			}
			return w.Flush()
		},
	}
	c.Flags().StringVar(&asOf, "as-of", "", "YYYY-MM-DD")
	return c
}

func journalCmd() *cobra.Command {
	var timeFilter, from, to string
	c := &cobra.Command{
		Use: "journal [ledger]", Short: "Journal", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolvePeriod(timeFilter, from, to)
			if err != nil {
				return err
			}
			return withLedgers(args, func(l *engine.Ledger) error {
				fmt.Printf("== %s ==", l.Name)
				if !r.Empty() {
					fmt.Printf("  [%s]", r.Label())
				}
				fmt.Println()
				for _, e := range l.Journal(r.Start, r.End) {
					switch e.Kind {
					case "txn":
						fmt.Printf("%s * %s\n", e.Date.Format("2006-01-02"), formatPayeeNarration(e.Payee, e.Narration))
						for _, p := range e.Postings {
							if p.Units == nil || p.Units.Commodity == "" && p.Units.Number.Sign() == 0 {
								fmt.Printf("  %s\n", p.Account)
								continue
							}
							fmt.Printf("  %-40s %s %s\n", p.Account, p.Units.Number.FloatString(4), p.Units.Commodity)
						}
					case "note":
						fmt.Printf("%s note %s %q\n", e.Date.Format("2006-01-02"), e.Account, e.Comment)
					case "event":
						fmt.Printf("%s event %q %q\n", e.Date.Format("2006-01-02"), e.Narration, e.Comment)
					}
				}
				return nil
			})
		},
	}
	addTimeFlags(c, &timeFilter, &from, &to)
	return c
}

func pnlCmd() *cobra.Command {
	var timeFilter, from, to string
	c := &cobra.Command{
		Use: "pnl [ledger]", Short: "P&L for a Fava-style period", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolvePeriod(timeFilter, from, to)
			if err != nil {
				return err
			}
			return withLedgers(args, func(l *engine.Ledger) error {
				fmt.Printf("== %s ==", l.Name)
				if !r.Empty() {
					fmt.Printf("  [%s]", r.Label())
				}
				fmt.Println()
				inc, exp := l.PnLTree(r.Start, r.End)
				printPnLTree := func(title string, lines []engine.PnLLine) {
					fmt.Println(title)
					for _, ln := range lines {
						pad := strings.Repeat("  ", ln.Depth)
						mark := "  "
						if ln.IsRollup {
							mark = "Σ "
						}
						name := ln.Name
						if name == "" {
							name = ln.Account
						}
						fmt.Printf("%s%s%-28s %s %s\n", pad, mark, name, ln.Amount.FloatString(2), ln.Commodity)
					}
				}
				printPnLTree("Income:", inc)
				printPnLTree("Expenses:", exp)
				return nil
			})
		},
	}
	addTimeFlags(c, &timeFilter, &from, &to)
	return c
}

// addTimeFlags registers Fava-style --time plus optional --from/--to overrides.
func addTimeFlags(c *cobra.Command, timeFilter, from, to *string) {
	c.Flags().StringVar(timeFilter, "time", "", "Fava-style period: 2024, 2024-03, 2024-Q1, month, month-1, year, 2020 - 2024-06")
	c.Flags().StringVar(from, "from", "", "inclusive start YYYY-MM-DD (overrides --time start if set alone with --to)")
	c.Flags().StringVar(to, "to", "", "inclusive end YYYY-MM-DD")
}

// resolvePeriod prefers --time; if empty, uses --from/--to; if both empty, all time.
func resolvePeriod(timeFilter, from, to string) (period.Range, error) {
	if timeFilter != "" {
		if from != "" || to != "" {
			return period.Range{}, fmt.Errorf("use either --time or --from/--to, not both")
		}
		return period.Parse(timeFilter, time.Now())
	}
	f, err := engine.ParseDate(from)
	if err != nil {
		return period.Range{}, err
	}
	t, err := engine.ParseDate(to)
	if err != nil {
		return period.Range{}, err
	}
	raw := ""
	if from != "" || to != "" {
		raw = from + " … " + to
	}
	return period.Range{Start: f, End: t, Raw: raw}, nil
}

func networthCmd() *cobra.Command {
	var asOf string
	c := &cobra.Command{
		Use: "networth [ledger]", Short: "Net worth", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := engine.ParseDate(asOf)
			if err != nil {
				return err
			}
			if t.IsZero() {
				t = engine.AsOfLatest
			}
			return withLedgers(args, func(l *engine.Ledger) error {
				lines, total, err := l.NetWorthTree(t)
				if err != nil {
					return err
				}
				fmt.Printf("== %s net worth (%s) ==\n", l.Name, l.OpCurrency)
				for _, ln := range lines {
					pad := strings.Repeat("  ", ln.Depth)
					mark := "  "
					if ln.IsRollup {
						mark = "Σ "
					}
					name := ln.Name
					if name == "" {
						name = ln.Account
					}
					flag := ""
					if ln.Unpriced {
						flag = " (no px)"
					}
					units := ""
					if ln.Units != nil {
						units = ln.Units.FloatString(4) + " " + ln.Commodity
					}
					fmt.Printf("%s%s%-28s %16s => %s %s%s\n",
						pad, mark, name, units, ln.Value.FloatString(2), l.OpCurrency, flag)
				}
				fmt.Printf("TOTAL %s %s\n", total.FloatString(2), l.OpCurrency)
				return nil
			})
		},
	}
	c.Flags().StringVar(&asOf, "as-of", "", "YYYY-MM-DD")
	return c
}

func accountCmd() *cobra.Command {
	var timeFilter, from, to string
	c := &cobra.Command{
		Use:   "account <ledger> <account>",
		Short: "Show one account (balance, period change, journal)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := resolvePeriod(timeFilter, from, to)
			if err != nil {
				return err
			}
			cwd, err := projectCwd()
			if err != nil {
				return err
			}
			p, pdb, pdiags, err := engine.OpenProject(cwd)
			if err != nil {
				return err
			}
			printDiags(pdiags)
			l, err := engine.OpenLedger(p, pdb, args[0])
			if err != nil {
				return err
			}
			acct := args[1]
			asOf := r.End
			if asOf.IsZero() {
				asOf = engine.AsOfLatest
			}
			fmt.Printf("== %s · %s ==", l.Name, acct)
			if !r.Empty() {
				fmt.Printf("  [%s]", r.Label())
			}
			fmt.Println()
			fmt.Println("Balance:")
			bals := l.AccountBalances(acct, asOf)
			if len(bals) == 0 {
				fmt.Println("  (zero)")
			} else {
				var cs []string
				for c := range bals {
					cs = append(cs, c)
				}
				sort.Strings(cs)
				w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
				for _, c := range cs {
					fmt.Fprintf(w, "  %s\t%s\n", bals[c].FloatString(6), c)
				}
				w.Flush()
			}
			fmt.Println("Change in period:")
			act := l.AccountActivity(acct, r.Start, r.End)
			if len(act) == 0 {
				fmt.Println("  (none)")
			} else {
				var cs []string
				for c := range act {
					cs = append(cs, c)
				}
				sort.Strings(cs)
				w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
				for _, c := range cs {
					fmt.Fprintf(w, "  %s\t%s\n", act[c].FloatString(6), c)
				}
				w.Flush()
			}
			fmt.Println("Journal:")
			for _, e := range l.JournalForAccount(acct, r.Start, r.End) {
				if e.Kind != "txn" {
					fmt.Printf("%s %s %s\n", e.Date.Format("2006-01-02"), e.Kind, e.Comment)
					continue
				}
				fmt.Printf("%s * %s\n", e.Date.Format("2006-01-02"), formatPayeeNarration(e.Payee, e.Narration))
				for _, p := range e.Postings {
					mark := "  "
					if p.Account == acct {
						mark = "* "
					}
					if p.Units == nil {
						fmt.Printf("%s%s\n", mark, p.Account)
						continue
					}
					fmt.Printf("%s%-40s %s %s\n", mark, p.Account, p.Units.Number.FloatString(4), p.Units.Commodity)
				}
			}
			return nil
		},
	}
	addTimeFlags(c, &timeFilter, &from, &to)
	return c
}

// formatPayeeNarration prints Beancount-style "Payee" "Narration" or a single string.
func formatPayeeNarration(payee, narration string) string {
	switch {
	case payee != "" && narration != "":
		return fmt.Sprintf("%q %q", payee, narration)
	case payee != "":
		return fmt.Sprintf("%q", payee)
	default:
		return fmt.Sprintf("%q", narration)
	}
}

func parseCmd() *cobra.Command {
	return &cobra.Command{
		Use: "parse <file>", Short: "Dump directives from a file", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			dirs, diags, err := parser.Parse(args[0], src)
			printDiags(diags)
			if err != nil {
				return err
			}
			for _, d := range dirs {
				fmt.Printf("%T date=%s\n", d, d.GetDate().Format("2006-01-02"))
			}
			return nil
		},
	}
}

// ingestCmd: contapila ingest --file path [-- CMD args…]
// JSONL directives on producer stdout (or contapila stdin if no --).
// With --, contapila stdin is passed through to CMD.
func ingestCmd() *cobra.Command {
	var file string
	c := &cobra.Command{
		Use:   "ingest --file <path> [-- CMD [args…]]",
		Short: "Merge JSONL directives into a beancount file",
		Long: `Read a stream of JSONL directive objects and merge them into --file.

Without --, JSONL is read from contapila stdin.
With -- CMD args, runs CMD (stdin passed through) and reads JSONL from CMD stdout.
Logs from CMD should go to stderr.

Each JSON line is one directive (full AST-shaped fields). Optional "id" becomes
metadata ingest_id for upsert; without id, lines are appended.
Any error or non-zero CMD exit aborts with no write.`,
		Args:                  cobra.ArbitraryArgs,
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if file == "" {
				return fmt.Errorf("--file is required")
			}
			var (
				incoming []ast.Directive
				err      error
			)
			// args after -- are the producer command
			if len(args) > 0 {
				ex := exec.Command(args[0], args[1:]...)
				ex.Stdin = os.Stdin
				ex.Stderr = os.Stderr
				stdout, errPipe := ex.StdoutPipe()
				if errPipe != nil {
					return errPipe
				}
				if err := ex.Start(); err != nil {
					return err
				}
				incoming, err = ingest.DecodeJSONL(stdout, os.Stderr)
				waitErr := ex.Wait()
				if err != nil {
					return err
				}
				if waitErr != nil {
					return fmt.Errorf("producer failed: %w", waitErr)
				}
			} else {
				incoming, err = ingest.DecodeJSONL(os.Stdin, os.Stderr)
				if err != nil {
					return err
				}
			}

			existing := ""
			if b, rerr := os.ReadFile(file); rerr == nil {
				existing = string(b)
			} else if !os.IsNotExist(rerr) {
				return rerr
			}

			out, err := ingest.Apply(existing, file, incoming)
			if err != nil {
				return err
			}
			return ingest.WriteFileAtomic(file, []byte(out))
		},
	}
	c.Flags().StringVar(&file, "file", "", "target beancount file (created on success if missing)")
	if err := c.MarkFlagRequired("file"); err != nil {
		panic(fmt.Sprintf("MarkFlagRequired(file): %v", err))
	}
	return c
}

func webCmd() *cobra.Command {
	var addr string
	c := &cobra.Command{
		Use: "web [ledger]", Short: "Read-only web UI", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := projectCwd()
			if err != nil {
				return err
			}
			p, pdb, _, err := engine.OpenProject(cwd)
			if err != nil {
				return err
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			return web.Listen(p, pdb, name, addr)
		},
	}
	c.Flags().StringVar(&addr, "addr", "127.0.0.1:8765", "listen address (host:port)")
	return c
}

func lspCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lsp",
		Short: "Language server (stdio) for Helix and other LSP clients",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Protocol on stdout; keep slog on stderr.
			return lsp.RunStdio(cmd.Context())
		},
	}
}
