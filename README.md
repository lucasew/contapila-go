# Contapila

Local, one-binary **Beancount-class** ledger engine and **read-only web UI** (Fava-like reports, not a Fava clone).

Quiet · precise · numbers first. Aimed at personal double-entry bookkeepers who keep Beancount-style journals and want fast `check` plus balances / journal / P&L / net worth without a Python Beancount runtime.

## Status

MVP in progress. Full language/engine contract: [`SPEC.md`](SPEC.md). Product brief: [`PRODUCT.md`](PRODUCT.md). Web UI direction: [`DESIGN.md`](DESIGN.md).

## Features (MVP)

- Parse and book real `.beancount` journals (plugin-free subset)
- Multi-ledger **project** layout with CUE config (`contapila.cue`)
- CLI reports: check, balances, journal, P&L, net worth, account
- `ingest` — merge JSONL directives into a beancount file
- `dump` — PDF/XLSX hierarchy as compact JSON for stdlib extract scripts
- `web` — localhost read-only HTTP UI (Go templates + dense charts)
- Single Go binary; no plugins

Default inventory model is **merged average-cost** (documented product policy; may differ from upstream Beancount on plugin-free ledgers that never set a booking method). See SPEC §2.2.

## Install / build

Requires [mise](https://mise.jdx.dev/) (or install Go / Bun yourself and mirror `mise.toml`).

```bash
mise run install          # go mod tidy + bun install
mise run ci               # tests + build + CSS codegen
go build -o contapila ./cmd/contapila/
```

Binary can also be installed from GitHub releases when published (`lucasew/contapila` / workspaced catalog name `contapila`).

## Quick start

Point at a project root (directory containing `contapila.cue` and ledger folders):

```bash
contapila -C path/to/project status
contapila -C path/to/project check
contapila -C path/to/project balances personal
contapila -C path/to/project web
```

`-C` / `--directory` is like `git -C`: run as if started in that directory.

Example project: [`testdata/example`](testdata/example) (see [`testdata/README.md`](testdata/README.md)).

```bash
contapila -C testdata/example check
contapila -C testdata/example web
```

## CLI overview

| Command | Purpose |
|---------|---------|
| `status` | Project / ledger discovery |
| `check [ledger]` | Validate (all ledgers if omitted) |
| `balances [ledger]` | Balances as-of |
| `journal [ledger]` | Period activity |
| `pnl [ledger]` | Income vs expenses |
| `networth [ledger]` | Net worth as-of |
| `account …` | Account-focused views |
| `parse` | Parse diagnostics |
| `ingest --file path [-- CMD …]` | JSONL → beancount merge |
| `dump <dialect> <path>` | PDF/XLSX element tree → compact JSON (`--password` for encrypted files) |
| `web [ledger]` | Read-only HTTP UI |

Ledger arguments are **directory names** under the project root (e.g. `personal`, `acme`).

## Development

```bash
mise run format          # go fmt
mise run codegen         # rebuild embedded CSS (bun)
mise run ci
```

Module path: `github.com/lucasew/contapila-go`.

## Docs map

| Doc | Role |
|-----|------|
| [`SPEC.md`](SPEC.md) | Language, project layout, booking, reports |
| [`PRODUCT.md`](PRODUCT.md) | Users, personality, design principles |
| [`DESIGN.md`](DESIGN.md) | Web UI density, theme, charts |
| [`testdata/README.md`](testdata/README.md) | Example / kitchensink fixtures |

## License

See repository license file when present; otherwise treat as the author’s project defaults until a LICENSE is added.
