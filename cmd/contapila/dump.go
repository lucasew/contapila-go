package main

import (
	"fmt"
	"strings"

	"github.com/lucasew/contapila-go/internal/dump"
	_ "github.com/lucasew/contapila-go/internal/dump/pdfdslipakv1"
	_ "github.com/lucasew/contapila-go/internal/dump/xlsxexcelizev1"
	"github.com/spf13/cobra"
)

func dumpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dump <dialect> <path>",
		Short: "Dump a source document as a versioned JSON element tree",
		Long: `Dump PDF or spreadsheet structure as compact JSON for stdlib-only extract scripts.

Dialect ids are $format-$lib-v$n (also present in the JSON envelope):

` + dialectHelp() + `

Output is one compact JSON object on stdout:

  {"dialect":"…","source":"<path-as-given>","data":{"type":"…","children":[…]}}

Pipe into a language-stdlib script, then into contapila ingest as JSONL directives.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dialect, path := args[0], args[1]
			if _, ok := dump.Lookup(dialect); !ok {
				return fmt.Errorf("unknown dialect %q\n\n%s", dialect, dumpUsageHint())
			}
			data, err := dump.Extract(dialect, path)
			if err != nil {
				return err
			}
			out, err := dump.MarshalCompact(data)
			if err != nil {
				return fmt.Errorf("marshal json: %w", err)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return err
		},
		SilenceUsage: true,
	}
}

func dialectHelp() string {
	ids := dump.Dialects()
	if len(ids) == 0 {
		return "  (no dialects registered)"
	}
	var b strings.Builder
	for _, id := range ids {
		b.WriteString("  - ")
		b.WriteString(id)
		b.WriteByte('\n')
	}
	return b.String()
}

func dumpUsageHint() string {
	return "Usage: contapila dump <dialect> <path>\nKnown dialects:\n" + dialectHelp()
}
