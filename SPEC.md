# Contapila — Specification

Status: MVP implementation in progress (tree-sitter grammar wired via modernc-tree-sitter/ccgo-tree-sitter).

Contapila is a self-contained **Go** reimplementation of a Beancount-class ledger engine plus a Fava-class read-only web UI (headless `web` or **eletrocromo** desktop shell) and an optional **language server**: **one binary** (Cobra CLI + HTTP server with Go templates + `contapila lsp` + `contapila desktop`). Philosophy is **Helix, not Neovim**: good defaults, batteries included, no plugin system, poetic license on tooling.

---

## 1. Goals

| Goal | Detail |
|------|--------|
| Self-contained | Single Go binary; no Python Beancount at runtime; embedded CUE |
| Drop-in language | Parse and interpret real `.beancount` journals with high fidelity |
| Semantics bar **B** | Same balances/lots on **plugin-free** ledgers for supported features; document intentional divergences |
| Ready to use | Enough reports for a normal person: month-end balances, activity, P&L, net worth, `check` |
| Project-oriented | Git-like project root + conventional multi-ledger layout |
| Editor-ready | Same project truth in Helix via `contapila lsp` (check, account goto, account/commodity completion, minimal hover) |

### Non-goals (MVP)

- Python plugin compatibility
- Full BQL / `bean-query` parity
- Fava editor / write-back
- Multi-user auth / remote multi-tenant hosting
- Tooling flag-compatibility with upstream Beancount CLIs
- Second/temporary parser before modernc grammar lands
- Full CUE language server (cuepls remains separate; contapila may only surface project-load errors on `contapila.cue` if free)
- Separate `contapila-lsp` binary

---

## 2. Compatibility

### 2.1 Contract

- **In scope:** syntax + loader + booking + validation for the MVP directive set, without plugins.
- **Tooling:** poetic license (Cobra command names/flags need not match `bean-*`).
- **Plugins:** none. Unknown/unsupported constructs: **warn + skip** via `log/slog` where safe; **error** when continuing would corrupt inventory or lie about balances.

### 2.2 Intentional divergence: booking default

Upstream Beancount is lot-centric; average-cost is not its comfortable default.

Contapila defaults to **merged average-cost inventory** (model A below), aimed at real use (e.g. Receita Federal / preço médio for equities). Files that never set a booking policy may **disagree** with Beancount on inventories and gains. This is documented product policy, not an accident.

When / if `option "booking_method"` (or CUE equivalent) appears, honor it only for methods actually implemented; unsupported method → clear load/`check` error.

---

## 3. Product surface (MVP)

### 3.1 CLI (Cobra)

Illustrative commands (names may be refined at implement time):

| Command | Behavior |
|---------|----------|
| `contapila check [ledger]` | Validate; all ledgers if name omitted |
| `contapila balances [ledger]` | Balances as-of date |
| `contapila journal [ledger]` | Period journal / activity |
| `contapila pnl [ledger]` | Income vs expenses for a period |
| `contapila networth [ledger]` | Net worth as-of (shared prices) |
| `contapila ingest --file path [-- CMD …]` | Merge JSONL directives into a beancount file (upsert by `id` → `ingest_id`) |
| `contapila dump <dialect> <path>` | Dump PDF/XLSX element tree as compact JSON (`dump` + dialect subcommand `$format-$lib-v$n`; `--password` for encrypted files; for stdlib-only extract scripts → ingest) |
| `contapila web [ledger]` | Read-only HTTP UI (headless; owns bind via `--addr`) |
| `contapila desktop [ledger]` | Same UI via **eletrocromo** (Helium `--app` window; library owns loopback bind + token auth) |
| `contapila lsp` | Language server over stdio (Helix dogfood; see §3.4) |

Ledger argument is the **directory name** under the project root (see §4). Project root is always from `-C` / process cwd (walk up for `contapila.cue`); neither `web` nor `desktop` takes a project path positional.

### 3.2 Web server

- **Read-only** viewer over the same `*Ledger` APIs as the CLI.
- **Go templates** (server-rendered).
- **`web`:** default bind `127.0.0.1:8765` (`--addr`); no app-window shell. No multi-user auth story (local tool).
- **`desktop`:** same HTTP handler as `web`; no `--addr`. Bind, one-shot token auth, and window lifetime are owned by **[eletrocromo](https://github.com/lewtec/eletrocromo)** (`App.ID` = `br.tec.lew.contapila`). Never fall back to the system browser or to `web --addr` if Helium is missing.
- **Live reload** (watch ledger includes + prices + config): nice-to-have, not blocking first server slice.
- Out of MVP: in-browser edit, multi-user, write-back.

#### 3.2.1 Desktop auto-launch (QoL)

**Intent:** double-click / “Open with contapila” opens the project UI without a wrapper script; typing in a real terminal stays CLI-first.

| Mode | When | Behavior |
|------|------|----------|
| Explicit | `contapila desktop [ledger]` | Always eletrocromo, regardless of TTY |
| Implicit rewrite | **Both** stdin and stdout are **not** TTYs, and argv is bare or a single project path | Rewrite to `desktop` (see below) |
| CLI default | Either stdin or stdout is a TTY, or argv is a real subcommand / other shape | Normal Cobra (help, `status`, `web`, …) |

**Terminology:** “not a TTY” = process not attached to an interactive terminal on that fd (`isatty` / `term.IsTerminal`). Dual stdin+stdout check reduces false positives from pipes/CI (`contapila \| …`, redirected stdout).

**Implicit rewrite mapping:**

| User runs (not dual-TTY) | Becomes |
|--------------------------|---------|
| `contapila` (no positionals) | `desktop` with current `-C` / cwd |
| `contapila /path/to/project` | work dir = that directory → `desktop` |
| `contapila /path/to/contapila.cue` | work dir = **parent** of the cue file → `desktop` |
| `contapila -C /path` (no positionals) | `desktop` (`-C` already set) |
| `contapila status` / `web` / two+ args / unknown junk | **no** rewrite — normal Cobra |

Ledger is **not** accepted on the implicit bare path; only via `desktop [ledger]` or `web [ledger]`.

**Project marker:** `contapila.cue` (same walk-up discovery as the rest of the CLI).

**Failures** (missing marker, bad project, Helium/ensure/`App.Run` error): message on **stderr**, exit **1**. No silent fall-through to help on the implicit path (help is useless without a terminal).

**Layout (v1):** wiring lives in `cmd/contapila`; extract `internal/desktop` only if it grows. Tests: pure helpers for auto-launch gate + path→work-dir; **no** Helium window in contapila CI (eletrocromo’s own tests cover host launch).

**Dependency:** `github.com/lewtec/eletrocromo`.

### 3.3 Reports

| Report | Question |
|--------|----------|
| Balances as-of | What is in each account on date D? |
| Journal / activity | What moved in period [from, to]? |
| P&L | Income vs expenses for the period (by account type prefix) |
| Net worth | Assets − liabilities in operating currency as-of D |
| Check | Opens/closes, balance assertions, booking errors, unbalanced txns |

### 3.4 Language server (LSP)

Status: **specified; first dogfood cut not yet shipped.** Same binary, stdio LSP. Dogfood target: **Helix**. Example projects may ship `.helix/languages` (or equivalent) pointing at `contapila lsp`.

#### 3.4.1 First dogfood cut (definition of done)

| Capability | Behavior |
|------------|----------|
| `textDocument/publishDiagnostics` | **Two channels** — see §3.4.3 |
| `textDocument/definition` | Account use → that ledger’s `open`; missing → editor “no definition” / status feedback (no fake jump to first posting) |
| `textDocument/completion` | Accounts + commodities only where the grammar expects an account or commodity slot |
| `textDocument/hover` | **Minimal** — account: `open` date + currencies/meta; commodity: CUE policy already on the loaded project (precision, class, …). No live balances/lots |

**Out of first dogfood cut:** commodity goto, references, rename, formatting, code actions/fixits, semantic tokens, workspace symbols, rich/Fava-ish hover, full CUE IDE features.

Later phases may add the rest of a normal language server surface; v1 stops at the table above.

#### 3.4.2 Project model (LSP)

| Rule | Behavior |
|------|----------|
| Unit of truth | **Whole contapila project** (all ledgers + shared journals + CUE), not a single buffer |
| Root discovery | **Same as CLI**: walk up from the document path for `contapila.cue`; nearest wins |
| Multi-project | **First resolved root wins** for the server process; no multi-session map |
| Open buffers | LSP text overlays **win** over disk on every recompute |
| Closed files | Re-read from disk each recompute turn (debounce/save). **fsnotify optional** — may wake debounce; absence is fine (next turn still refreshes) |
| Account symbols | **Ledger of the current file** only (inventory isolation) |
| Commodity symbols | **Project-shared** (prices/indexes/root journals still commodity-aware) |
| Non-ledger `.beancount` | No fake ledger account chart; commodities still resolve; parse + project load as applicable |
| `contapila.cue` | **Not** a CUE language server target. Config changes still invalidate project state when noticed. Surfacing unify/load errors that already point at the cue file is allowed if cheap |
| Open ledger scope | Opening any file that belongs to ledger L → **ingest whole L** (main + includes + stream/prices as engine already does). **Publish diagnostics for all files of open ledgers**, not only the focused buffer |

No re-open model for accounts: definition is the single `open`.

#### 3.4.3 Recompute: two-tier + snapshot swap

```text
didChange / didSave / optional fsnotify wake
        │
        ├─ fast: parse dirty buffer(s) → publish parse/syntax diagnostics immediately
        │
        └─ if dirty: debounce and/or save
                │
                ├─ parse fails → keep last-good account/commodity indexes + last-good check diags
                │                 (completion/goto/hover still use last-good index)
                │
                └─ parse succeeds → background full project perception (overlays + disk)
                                   rebuild indexes + run check (ctx-cancellable)
                                   atomic swap: indexes + semantic/check diagnostics
```

| Rule | Behavior |
|------|----------|
| Triggers | **Debounce and save**, only when something actually changed (dirty) |
| Overlap | **Cancel + restart** via `context.Context` (newer edit/save cancels in-flight slow run) |
| Index / check publish | Only after parse **passes**; atomic swap of the live snapshot |
| Parse publish | Immediate (does not wait for successful full check) |
| Partial trees | **No** best-effort symbol extraction from broken parses — last-good index only |

#### 3.4.4 Completion and navigation detail

- **Completion contexts:** only syntactic positions where an **account** or **commodity** is expected (posting account, `open`/`close`/`balance`/`pad`/`document` account fields, amount commodity, `price` commodities, `commodity` directive, etc.). Not narration / free text.
- **Account completion:** opens from the **current file’s ledger** (last-good index).
- **Commodity completion:** project commodity set (last-good index), including when editing `prices.beancount` and other shared journals.
- **Goto:** account → `open` in that ledger’s graph; no definition → client-visible error, not a degraded first-mention jump.
- **Hover:** index/config facts only (see §3.4.1); must not force a full booking pass on every hover.

#### 3.4.5 Locations and ranges

- Prefer real ranges from existing AST spans (`Meta.StartByte` / `EndByte`) and `grammar.LineIndex` (`LineColumnAt`).
- LSP positions require protocol encoding conversion (tree-sitter / line index are **byte** offsets; LSP commonly **UTF-16**).
- Line-only diagnostics only as fallback when span is unknown (synthesized nodes, legacy diags without bytes).

#### 3.4.6 Architecture seams (required for parity)

| Seam | Intent |
|------|--------|
| FS-shaped loader dependency | Project open / include load take an FS-like reader. **CLI** = disk; **LSP** = overlay FS (buffer text first, else disk). One load/check path for both |
| Surfaces | LSP is another consumer of project/`Ledger` APIs — same check truth as `contapila check` |
| Context | Thread `context.Context` through load/check far enough that cancel aborts in-flight LSP work without publishing stale results |

Not: temp-dir materialization of overlays; not a forked LSP-only parse/check that drifts from CLI.

#### 3.4.7 Protocol stack (library choice)

| Choice | Detail |
|--------|--------|
| Modules | [`go.lsp.dev/protocol`](https://pkg.go.dev/go.lsp.dev/protocol) + [`go.lsp.dev/jsonrpc2`](https://pkg.go.dev/go.lsp.dev/jsonrpc2) (LSP **3.18**, generated types) |
| Server API | Embed `protocol.UnimplementedServer`; serve with `protocol.NewServer`; use typed `Client` for `publishDiagnostics` and window messages |
| Not | Full SDKs (e.g. tliron/glsp, TobiasYin/go-lsp) as primary stack; gopls `internal/*` (unimportable); hand-rolled Content-Length framing |
| Toolchain | These modules require **Go 1.26+** — bump the contapila module when implementing LSP |
| Isolation | LSP imports live under `internal/lsp` only; engine/loader stay free of protocol types |
| Stdio | Wrap `os.Stdin` / `os.Stdout` as a `jsonrpc2.Stream` (stdout is protocol-only) |

**Logging / user feedback**

| Channel | Use |
|---------|-----|
| stdout | LSP protocol only |
| stderr `log/slog` | Operator / debug (same family as CLI; respect `--verbose` if shared) |
| `window/logMessage` | Soft user-relevant warnings that are not diagnostics |
| `window/showMessage` | Rare hard failures (e.g. project open broken) |
| diagnostics / request results | Product truth (check errors; missing definition → empty result / client “no definition”, not a fake status-bar API) |

There is no portable LSP “set status bar text”; do not design around editor-private chrome.

**Testing**

| Layer | Method |
|-------|--------|
| Regression | In-memory `jsonrpc2.Stream` (pipe) client ↔ `NewServer`; assert completion, definition, hover, diagnostics on fixtures |
| Unit | Index / goto / hover helpers without RPC where possible |
| Acceptance | Helix dogfood + example `.helix` config (§3.4.8) |

#### 3.4.8 Client packaging

- Document / ship Helix `language-server` config for Beancount (and related journal paths as needed) invoking `contapila lsp`.
- Prefer embedding example config under testdata or docs so dogfood is one clone away.

---

## 4. Project layout

### 4.1 Root discovery

- Walk **upward from the process CWD** looking for `contapila.cue` (same idea as git finding `.git`).
- Nearest file wins; its directory is the **project root**.
- If none found → error (`not a contapila project`).
- **No `--config` flag.**

### 4.2 Convention

```text
<root>/
  contapila.cue           # required project marker; may be empty
  prices.beancount        # shared prices (empty/missing → warn)
  indexes.beancount       # shared index series for autointerest (optional; auto-injected)
  personal/
    main.beancount        # ledger name = "personal"
  empresa/
    main.beancount        # ledger name = "empresa"
  scratch/                # no main.beancount → ignore
```

| Rule | Behavior |
|------|----------|
| Config marker | `contapila.cue` at project root; **empty file is valid** (prelude supplies defaults) |
| Ledgers | Exactly one level: `<root>/*/main.beancount` |
| Ledger name | **Directory name** |
| Dir without `main.beancount` | **Ignore** |
| Recursive `**/main.beancount` | **No** |
| Root-level `main.beancount` | Not an entrypoint |
| Zero ledgers found | Error when running check/web/reports |
| Shared root journals | CUE `project_journals` (prelude defaults: `prices.beancount` + `indexes.beancount`) |
| `role: "prices"` | Load into shared PriceDB; missing → **warn** by default |
| `role: "stream"` | Auto-inject into every ledger stream (no `include` required); missing → ignore by default |
| Includes | Paths relative to the **including file's directory**; globs allowed |
| Optional root commodities | `<root>/commodities.beancount` — often `include`d from ledgers (not in default `project_journals`) |

**Price DB:** for a given (base, quote, date), **last write wins** when loading prices journals (and if the same day appears twice).

**`project_journals` (prelude):** list of `{path, role, missing}` relative to the project root. Override the whole list in `contapila.cue` to add/remove auto-imports. Explicit ledger `include` of the same realpath is not double-loaded for `stream` roles.

Ledgers may still `include "../prices.beancount"` / `include "../commodities.beancount"` for journal-visible copies; PriceDB still comes from `role: "prices"` journals.

### 4.3 Isolation and sharing

| Concern | Scope |
|---------|--------|
| Inventory, transactions, pads, balance assertions, accounts (`open`/`close`) | **Per ledger** (isolated) |
| Commodity policy (precision, tolerance, class) | **Shared** (project CUE) |
| Market `price` directives | **Shared** (`project_journals` role `prices` → one PriceDB for all ledgers) |
| Index series (`custom "index"`) | **Shared** (`project_journals` role `stream` auto-injected into each ledger) |

Multiple entrypoints are **named parallel ledgers**, never merged into one inventory.

### 4.4 Account documents (`<ledger>/docs/by-account`)

Documents are **per ledger** (same isolation as inventory). Layout:

```text
<root>/<ledger>/docs/by-account/<seg>/<seg>/…/<filename>
```

Account components become **subdirectories** (`:` → `/`):

Example: ledger `personal`, account `Assets:BR:Alfa:ContaCorrente` →  
`personal/docs/by-account/Assets/BR/Alfa/ContaCorrente/`.

**Filenames** start with a calendar date in **`yyyymmdd`**, then an optional
separator and rest of the name:

```text
20240301_statement.txt
20230810-INV-001.pdf
```

On ledger open the host walks **`<that-ledger>/docs/by-account/**`** and synthesizes
`document` directives (date from filename prefix, account from path). Explicit
`document` lines in that ledger’s journal merge in; same path prefers the explicit
directive. Account web UI lists documents and serves files under `/docfile/<ledger>/docs/…`.

Metadata `document: "…"` on transactions/postings is **stored** on the journal AST
and expanded into the ledger’s document list at open (same merge rules as filesystem
synth; path prefers explicit `document` directive). Not injected into CUE.

### 4.5 Ledgers in CUE (discovered) and inter-ledger links

On project open the host **looks up** `<root>/*/main.beancount` and injects a
generated CUE fragment (workspaced-style host data):

```cue
ledgers: close({
  personal: {name: "personal", main: "<abs>/personal/main.beancount"}
  acme:     {name: "acme",     main: "<abs>/acme/main.beancount"}
  // …
})
```

Types live in the embedded **prelude**:

| Type | Meaning |
|------|---------|
| `#Ledger` | `{name: #LedgerID, main: string}` — one discovered ledger |
| `#LedgerID` | Directory-name shape: `^[A-Za-z][A-Za-z0-9_-]*$` |
| `#LedgerName` | `or([for n, _ in ledgers {n}])` — **keys of the injected map only** |
| `#LedgerRef` / `#LedgerLink` | Cross-ledger endpoints using `#LedgerName` |

User `contapila.cue` does **not** list ledgers; inventing keys under `ledgers` fails (struct is `close`d). Links:

```cue
links: [{
  name: "acme-profit-distribution"
  from: {ledger: "acme", account: "Equity:DistribuicaoLucros"}
  to:   {ledger: "personal", account: "Income:Ativo:BR:DistribuicaoLucros:Acme"}
}]
```

**MVP:** CUE validates ledger **names** against discovery; `check` does **not** reconcile balances yet.

---

## 5. Architecture

### 5.1 Public API shape

- Open project from CWD → project handle (root, config, PriceDB, ledger names).
- Open/load each named ledger → `*Ledger`.
- Surfaces (CLI, HTTP, LSP) call only project/`Ledger` methods — no parsing in handlers.
- Project/loader accept an **FS-shaped** dependency for file reads (disk default; LSP overlays).

Suggested capabilities on `*Ledger`:

- `Check() error` (hard errors fail; warnings via slog)
- `Balances(asOf)`
- `Journal(from, to)`
- `PnL(from, to)`
- `NetWorth(asOf)` — uses shared PriceDB + operating currency rules

### 5.2 Pipeline (per ledger)

```text
resolve project root (contapila.cue)
load prices.beancount → PriceDB          # once per project
for each ledger name:
  parse main.beancount + include graph   # tree-sitter (deferred)
  split config-ish directives vs stream
  encode config facts → CUE
  unify: prelude & contapila.cue & ledgerFacts
  decode RuntimeConfig
  apply stream: tags/meta (none in MVP), booking, pads, assertions
  reports
```

Internal stages are separate packages; the public surface stays a deep module (single entry, hidden stages).

### 5.3 Parser bootstrap

- **Wait** for Beancount grammar via [modernc / ccgo-tree-sitter](https://github.com/modernc-tree-sitter/ccgo-tree-sitter).
- No temporary hand parser, no Python subprocess, no alternate long-term cgo binding as the product path.
- Design freezes the AST/config/booking contracts so the grammar drops into one adapter.

### 5.4 Numeric types

- Engine amounts and costs: **`math/big.Rat`** (never `float64` for money).
- Display/tolerance from commodity policy (§7).

### 5.5 Language server placement

- Command: **`contapila lsp`** (stdio) in the main module — not a second binary.
- Package boundary: dedicated LSP adapter (protocol, overlays, debounce, publish) over the same open/load/check pipeline as CLI.
- Wire stack: **`go.lsp.dev/protocol` + `go.lsp.dev/jsonrpc2`** (§3.4.7); not glsp-as-framework.
- Full feature matrix and recompute rules: **§3.4**.

---

## 6. CUE config plane

### 6.1 Runtime

- **Embed** CUE (`cuelang.org/go`), workspaced-style — no `cue` CLI required for normal use.
- Shipped **prelude** (schema, defaults, asset-class short-circuits) unified with user `contapila.cue` and **ledger-derived config facts**.
- **CUE decides** conflicts on the config plane (unification failure → load error). Do not implement ad hoc “who wins” tables in Go for config.

### 6.2 What goes into CUE

| In CUE (config plane) | Not in CUE |
|-----------------------|------------|
| Options (e.g. operating currency) | Transactions / postings |
| Commodities + precision/tolerance/class | Full `price` time series (volume) |
| Price **pair inventory** (`price_pairs` inject) | Individual price points / rates |
| Per-ledger account open/close facts | `balance`, `pad` |
| Project overlays in `contapila.cue` | `note`, `event`, journal stream |
| Prelude defaults | Include graph resolution (Go first) |
| (nothing for txn meta) | Txn/posting `key_value` metadata (Go journal only) |

### 6.3 Dual definition

- Commodities (and policy) may be declared in CUE and/or `commodity` directives; facts are encoded and **unified in CUE**.
- Accounts are **per ledger**: from that ledger’s `open`/`close` (and only that ledger’s facts in the per-ledger unify).
- Transactions are **never** executed or stored as CUE.

### 6.4 Minimal user file

```cue
// contapila.cue — valid empty project marker
// Optional overlays, e.g.:
// commodities: { BRL: {class: "fiat"}, BTC: {class: "crypto"} }
```

### 6.5 Defaults (prelude)

- Default **precision: 5** decimal places.
- Asset classes (illustrative; exact prelude schema at implement time) override precision (e.g. fiat → 2, crypto → 8).
- Default **tolerance**: half ULP of commodity `precision` (CUE `#Commodity` ⊔ journal commodity meta).
  Optional `tolerance` field overrides. Beancount `inferred_tolerance_*` options are **not** read.
- Undeclared commodities in journals: usable with prelude defaults (precision 5) unless stricter policy is added later.

---

## 7. Booking and inventory (model A)

### 7.1 Inventory model

- Per **account + commodity**: a **single merged average-cost** position (not multi-lot history).
- Lot theatre (FIFO/LIFO/STRICT multi-lot) is out of MVP unless explicitly reintroduced later.

### 7.2 Buys (increases)

- Inventory increases need a cost basis: **explicit cost** `{...}`, or **`@` / `@@` price** when braces are omitted.
- `@` → unit cost; `@@` → unit cost = total / units (same commodity as the price).
- `{...}` wins over `@`/`@@` when both are present.
- **Existing lot without braces:** if the account already holds that commodity with a cost basis, new units are booked at the **current average** (e.g. more USD into a costed USD cash account).
- New units merge into the average cost of the position.

### 7.3 Sells (reductions) — Shape 4

- Cost braces may be **omitted** on a reduction; engine books cost at **current average**.
- Prefer **`@@` total proceeds** for broker-style fills; support `@` unit price as well.
- Multi-stock sells: **one posting per commodity**; sugar is per line, not one average for the whole txn.
- **FX cash spend:** reducing a currency held at a *foreign* cost (e.g. USD with BRL average cost) to pay for legs that need that currency in face terms — stocks `@ … USD`, **USD expenses**, residual cash drain — weights the cash leg in **face currency (USD)** for balancing, while still reducing the FX lot’s foreign cost basis. Residual cash on a costed FX account also reduces inventory (not bare balance only). Pure FX conversion to BRL + gains still weights by cost basis.

Example:

```beancount
2024-03-10 * "Sell PETR4 + VALE3"
  Assets:Broker:PETR4  -40 PETR4 @@ 1400.00 BRL
  Assets:Broker:VALE3  -20 VALE3 @@ 1400.00 BRL
  Assets:Cash           2800.00 BRL
  Income:Gains
```

### 7.3a Booking order (same calendar day)

Directives are applied in order of:

1. **Date** ascending  
2. **Type rank** (Beancount-style): `open` → `pad` → `balance` → txn/note/event/… → `document` → `close`  
3. **Source line** when available  

So an `open` and a transaction on the same day book correctly even if includes put the txn earlier in the raw stream. A transaction **before** the open **date** is still an error.

### 7.3b Autointerest (indexed / fixed assets)

Built-in expander (not a plugin). On `open` with **`interest_rate`** (or alias **`interest-rate`**):

- Parse expression (spaces allowed): `115% CDI`, `IPCA + 10% aa`, `10% aa`, …  
  Daily growth uses `α × index_return + plus_daily` where `plus_daily = (1+r)^(1/n)−1` (`aa`→365, `am`→30).
- Counterpart income account: `Assets:…` → `Income:Passivo:…` (string replace); synth `open` if missing.
- **Materialize on `balance`:** insert **`pad` day-before** from that income account to the asset (skip if user already wrote a pad). Bank balance is ground truth.
- **Materialize on `close`:** inject **`pad` + `balance 0` CUR** (per open currency) before close so residual interest/principal zeros via Income:Passivo. Runs again **after** `closing: TRUE` autoclose (synthetic balance 0 / close).
- **Projection** (graphs / estimates): apply the curve using `custom "index" "CDI"|"IPCA" <daily_return>` in the stream; pure fixed also samples month-ends; horizon through `time.Now()`; **stops on `close`**.
- Index series: stream journals from `project_journals` (default `indexes.beancount`) are auto-injected; extra `custom "index"` may also appear via includes. No `fixes.beancount` write-back.

CUE `#Account` documents `interest_rate` and unifies hyphen alias onto snake_case when opens are injected.

### 7.4 Residual leg (no magic)

- At most **one** posting with **missing amount** absorbs the remainder (typically gains).
- That residual absorbs **every** unbalanced commodity: booked form expands to one amount per residual commodity on the residual account (source still has a single empty leg).
- **No** implicit default gains account; **no** auto-inserted source legs.
- Unbalanced transaction without an empty residual posting → **error**.
- Explicit `{cost}` on a sell that is not the current average (beyond tolerance) → **error**.
- Selling more units than inventory → **warn** (do not invent inventory; check still passes).

---

## 8. Operating currency and prices

### 8.1 Operating currency

1. Prefer explicit option / CUE option for `operating_currency`.
2. If missing: **warn**, then after includes are resolved, walk directives and take the commodity from the **first transaction that carries a posting amount commodity**.
3. If still none: currency-denominated reports error at report time.

### 8.2 Shared PriceDB

- Built from project `prices.beancount`.
- All ledgers consult the same DB for conversion / net worth.

### 8.3 Price lookup for as-of date D

Market conversion (net worth, charts, P&L when op currency is set) uses **PriceDB only**:

1. Direct pair `base→quote` on or before D (walk back: last price ≤ D).
2. Else inverse of `quote→base`.
3. Else one intermediate hop (e.g. `SPDW→USD→BRL`), each leg direct or inverse, both ≤ D.
4. If still missing: **warn**; market value is **0** (no cost-basis fallback).
5. Do not silently use future prices for past as-of dates.

Inventory cost basis (average cost, model A) remains for booking/gains; it is **not** used to value net worth.

### 8.4 Net worth

- Include **Assets** and **Liabilities** only (not Equity/Income/Expenses).
- Convert positions to operating currency with §8.3 using **signed** unit balances
  (Beancount: liabilities are usually credit → negative units; do **not** flip sign again).
- Valuation is **market only**; unpriced positions show as 0 with a UI/CLI “no px” marker.

---

## 9. Directives (MVP)

| Directive | MVP | Plane |
|-----------|-----|--------|
| `option` | yes | → CUE |
| `include` (+ globs) | yes | Go load |
| `commodity` | yes | → CUE |
| `open` / `close` | yes | → CUE (per ledger) |
| `*` / `!` transactions, postings | yes | Go |
| metadata on `open` / `commodity` | yes (stored; CUE `#Account` / `#Commodity` tandem) | Go + CUE |
| metadata on `price` | yes (stored on PriceDB points) | Go |
| metadata on `balance` | yes (journal stream only; **not** CUE) | Go |
| metadata on `event` | yes (journal stream only; **not** CUE) | Go |
| metadata on txn/posting | yes (journal stream only; **not** CUE) | Go |
| org-mode `section` / headlines (`* …`) | structure only — silent; nested directives collected | Go |
| posting `closing: TRUE` | yes — after residual fill, expands to `balance 0` + `close` next day for that account/commodity | Go |
| cost `{}`, price `@` / `@@` | yes | Go |
| cost `{amount, date}` | yes — books cost; also injects `price` on that date | Go |
| amount expressions (`+ - * /`, parens, unary `-`) | yes (grammar-complete) | Go |
| empty residual posting | yes | Go |
| `price` | yes (shared file → PriceDB; CUE `price_pairs` inventory only) | Go + CUE |
| `balance` | yes | Go |
| `pad` | yes | Go |
| `note` | yes | Go (store/display) |
| `event` | yes | Go (store/display) |
| `document` | yes (store/display; also synthesized from `<ledger>/docs/by-account`) | Go |
| `custom "index"` | yes — daily index return series for autointerest projection | Go |
| `custom` (other types) | yes (stored; unused types ignored by booking) | Go |
| `query` | no | — |
| `pushtag` / `poptag` / `pushmeta` / `popmeta` | no | — |
| `plugin` | no | — |
| unknown | warn + skip | — |

---

## 10. Diagnostics severity

Use **`log/slog`** for warnings. `check` fails only on **errors**.

| Event | Severity |
|-------|----------|
| Unopened account used | **warn** + allow |
| Posting after `close` | **error** |
| Posting `closing: TRUE` with no inferable commodity (unbooked / empty residual) | **error** |
| Posting `closing: TRUE` but `close` already written | **warn** (skip synthetic close; still assert `balance 0`) |
| Duplicate `open` same account | **error** |
| Unbalanced txn, no residual leg | **error** |
| Failed `balance` assertion | **error** |
| Over-sell (no inventory / not enough units) | **warn** (skip inventing inventory) |
| Bad average cost on reduce | **error** |
| Amount with number but no commodity | **error** (not residual) |
| Invalid `interest_rate` / `interest-rate` expression on open | **error** |
| Unknown `option` | **warn** |
| `include` literal path missing | **error** |
| `include` glob, zero matches | **warn** |
| Include cycle | **error** |
| Double-include same realpath | skip (dedupe); optional single warn |
| Missing `operating_currency` (inferred) | **warn** |
| Price missing for market conversion (value 0) | **warn** |
| `prices.beancount` empty/missing | **warn** |
| Unsupported / unknown directive | **warn** + skip |
| CUE config unify failure | **error** |

---

## 11. Includes

- Relative paths resolved against the **directory of the file containing the `include`**.
- Absolute paths allowed.
- Globs supported; zero matches → warn.
- Cycles → error.
- File identity for cycle/dedupe: realpath.

---

## 12. Verification strategy

| Phase | Method |
|-------|--------|
| Early | **Dogfood** on real multi-ledger projects |
| Ongoing | **Golden corpus** of fixtures (inputs + expected balances/errors/net worth) checked into the repo |
| Optional later | Python Beancount oracle for selected fixtures — not required for definition of done |

Golden fixtures should emphasize average-cost stock buys/partial sells, pads, includes, shared prices, multi-ledger isolation, and residual gains legs — not only STRICT lot puzzles.

---

## 13. Implementation order (when unblocked)

1. Repo scaffold: Go module, Cobra, embed CUE prelude, project root discovery + layout scan.
2. Config plane: prelude schema, empty `contapila.cue`, ledger config fact encoding (mock AST ok).
3. Parser adapter behind `Parse` once modernc grammar is available.
4. Loader (includes, streams) + booking model A + `check`.
5. PriceDB + reports (balances, journal, P&L, net worth).
6. CLI commands.
7. Read-only web server; live reload later.
8. Golden corpus expansion alongside dogfood.
9. **LSP dogfood (§3.4):** bump Go **1.26+** if needed → add `go.lsp.dev/protocol` + `jsonrpc2` → FS overlay seam + `context` on load/check → `contapila lsp` (`UnimplementedServer` + `NewServer`) → parse diags, snapshot indexes, account goto, account/commodity completion, minimal hover → in-memory RPC regression tests + Helix example config.
10. **Desktop shell (§3.2.1):** `contapila desktop` + not-a-TTY implicit rewrite via eletrocromo (same web handler).

---

## 14. Open at implement time (non-blocking)

- Exact CUE prelude schema field names and class set.
- Exact Cobra flag set (dates, output formats).
- HTTP routes and template structure.
- Whether `contapila init` scaffolds root + empty cue + sample ledger dirs.
- Tolerance combination rules when a transaction touches multiple commodities.
- Optional `option "booking_method"` surface once more methods exist.
- LSP debounce duration default; exact Go FS interface (`io/fs.FS` vs small project-local API).
- How aggressively to clear vs retain check diagnostics for ledgers with no open buffers.
- Exact `go.lsp.dev/*` module versions at implement time (track latest stable 3.18 stack).

---

## 15. Summary one-liner

**Contapila** = conventional multi-ledger Beancount project (`contapila.cue` + `*/main.beancount` + `prices.beancount`) with embedded CUE policy, average-cost inventory, and a single Go binary for check/reports/read-only web/**desktop (eletrocromo + Helium)**/**Helix LSP** — semantics-first, plugins never, tree-sitter via modernc.
