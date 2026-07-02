# docmill — Idiomatic Cleanup Review

A complete-codebase review aimed at three goals you set: **(1)** rename the
`pdfium` engine to something core to the product, **(2)** remove duplication, and
**(3)** tidy non-idiomatic code. Findings are grounded in the current tree
(`pkg/**`, `cmd/**`, `internal/**`); the separate cgo module `pdfium-oracle/`,
generated fakes, and `testdata/` are out of scope.

Leave inline comments on any item — agree, reject, re-scope, or pick a name.
Each item lists **scope**, **evidence** (file:line), **recommendation**, and
**risk**. Items are checkbox-tagged so we can track what lands.

---

## 0. Headline: the `pdfium` rename is almost free

The single most important finding for the rename: **`pkg/pdfium`'s public API
contains zero identifiers named "Pdfium"/"PDFium".** The exported surface is
`NewBackend`, `Backend`, `Document`, `Page`, `TextCells`, `WordTextCells`,
`RulingSegments`, … — all name-neutral (`pkg/pdfium/backend.go:21-249`).

So the rename is **not** an identifier migration. It is:

1. Rename the directory `pkg/pdfium/` → `pkg/<new>/`.
2. Rewrite the import-path string `…/pkg/pdfium` and `…/pkg/pdfium/internal/*`
   (8 root import sites; 3 non-test: `cmd/docmill/convert.go:10`,
   `cmd/docmill/acroform.go:11`, `internal/benchdp/corpus.go:13`).
3. Change the `package pdfium` clause (`backend.go:1`, `doc.go:11`, test files).
4. Update 5 cosmetic error strings `"native pkg/pdfium: …"`
   (`backend.go:101,185,286,288,290`).

### Two meanings of "pdfium" — do NOT mechanically find/replace

There are 353 occurrences of "pdfium" in product code, but most must **stay**:

- **Provenance comments** (~152 lines) like `// Ported from … @ pdfium 0db284a42`
  document fidelity to Google's PDFium C++ and must not be touched.
- **`TopLeftBoxToPDFiumBounds`** lives in `pkg/pdf/text.go:99`, **not** in the
  engine, and is shared by both the native backend and the cgo oracle
  (`pdfium-oracle/internal/pdfiumcore/backend.go:213`). It names a coordinate
  convention shared with real PDFium — **leave it**. A blind `Pdfium→New` sweep
  would break the oracle.
- **`pkg/pdf/oracle` and the `pdfium-oracle/` module** are real PDFium — exclude.
- **User-facing strings** `"initialise PDFium"` (`cmd/docmill/convert.go:31`),
  `"native PDFium document unavailable"` (`acroform.go:134`) name *our* engine
  with Google's brand. Deliberate call: rebrand to the new name, or keep "PDFium"
  as a recognisable label? (Recommend: rebrand — the port is ours now.)

**Safe sweep =** rename dir + rewrite the import-path token + `package` clause +
the 5 error strings, scoped to `pkg/pdfium/`, `cmd/docmill/`, `internal/benchdp/`;
explicitly exclude `pdfium-oracle/`, `pkg/pdf/oracle`, `TopLeftBoxToPDFiumBounds`,
and all `// Ported from … pdfium` comments.

### Pick a name

The engine *implements* the `pkg/pdf.Backend` interface — it is the PDF
parsing/interpretation layer. It must not collide with siblings `pdf`, `page`,
`render`, `table`, `geom`, `textline`, `telemetry`.

**Decided (review round 1): `pkg/parser`.** The engine becomes `pkg/parser`;
`pdfium.NewBackend()` → `parser.NewBackend()`. It reads cleanly against the
sibling `pkg/pdf` (which holds the `Backend`/`Document`/`Page` *interfaces*) —
`pkg/parser` is the concrete implementation that parses.

Two mechanical caveats to handle during R1 (neither is a blocker):

- The engine already contains `pkg/pdfium/internal/parser`; after the rename it
  becomes `pkg/parser/internal/parser`. Legal (distinct import paths) and stays
  hidden behind `internal/`, but the inner package keeps `package parser`, so
  any file that imports both will need an import alias. Low friction.
- The public entry type stays `Backend`/`Document`/`Page` (the API is
  name-neutral — see above). `parser.NewBackend()` returning a `Backend` is
  slightly unusual phrasing but correct; no need to rename the types.

- [x] **R0.** Engine name decided: `pkg/parser`.
- [ ] **R1.** Execute the scoped rename (dir `pkg/pdfium` → `pkg/parser` + import
  paths + `package` clause + the 5 error strings), excluding the
  provenance/oracle/`TopLeftBoxToPDFiumBounds` set above; add an import alias for
  the nested `internal/parser`. Verify with `go vet ./...`.

---

## 1. Remove duplication

Grounded, mechanical consolidations. `pkg/geom.Box` is **float64** (layout) and
`pkg/pdfium/internal/crt.FloatRect` is **float32** (exists to match the **cgo
PDFium oracle** bit-for-bit — per-op rounding, no FMA; see §5 for the "wasm"
naming correction). **Those two geometry worlds are legitimately separate — do
not merge them.** Everything below de-dups *within* a world or swaps in a stdlib
builtin.

### 1a. `verticalCenter` defined 3× → one `geom.Box` method
`(T+B)/2`, byte-identical, in three packages:
`pkg/pdf/headings.go:270` (`boxVerticalCenter`), `pkg/pdf/structure.go:307`,
`pkg/table/table.go:481`; plus `horizontalCenter` (`table.go:485`). `geom.Box`
has `Width/Height/Area/IoU/...` but **no center accessor** (`pkg/geom/geom.go`).
- **Rec:** add `func (b Box) CenterX() float64` / `CenterY() float64`; delete the
  three locals. `geom` is already imported at every site.
- **Risk:** none (pure refactor). One subtlety: these assume top-left origin
  (`T`/`B`); document that on the method.
- [ ] **D1.**

### 1b. `abs` / `absFloat` (float64) → `math.Abs`
`pkg/pdf/assemble.go:559` (`abs`) and `pkg/pdf/connect.go:362` (`absFloat`) are
identical. **Rec:** use `math.Abs`; delete both. **Risk:** none. — [ ] **D2.**

### 1c. Hand-rolled min/max → Go 1.21 builtins (module is go 1.26)
Native packages only: `pkg/pdf/headings.go:870` `maxInt(a,b)` → `max`;
`pkg/table/detect.go:3842` `minIndex(a,b)` → `min`;
`internal/benchdp/metrics.go:259` `min3(a,b,c)` → `min(a,min(b,c))`;
`pkg/table/table.go:494,504` `minInt([]int)`/`maxInt([]int)` → `slices.Min`/`slices.Max`.
Note `maxInt` currently means two different things (2-arg vs slice-reducer) —
removing both kills the collision.
- **Risk:** none for the native ones. **Leave the float32 `minf`/`maxf`/`min3`/
  `max3` in `crt`/`text`/`page`/`font`** — they sit on the oracle-parity path; a
  builtin swap is behaviourally identical but not worth the parity-review cost.
- [ ] **D3.**

### 1d. `roundf` duplicated *inside* `internal/font`
`pkg/pdfium/internal/font/load.go:366` `roundf(float32)` ==
`pkg/pdfium/internal/font/glyphsource.go:537` `roundf32(float32)`. **Rec:** keep
`roundf64` + thin `roundf32`, delete `load.go:roundf`, repoint callers. Leave
`round26` (fixed-point, different). **Risk:** low; same package. — [ ] **D4.**

### 1e. Repeated cell/box sort comparators (~25 sites)
`cells[i].Box.L < cells[j].Box.L` (~14×) and `a.Index < b.Index` (~12×) inlined
across `pkg/table/detect.go`, `grid.go`, `pkg/pdf/lineelement.go`,
`pkg/page/page.go:62`. `pkg/table/alignment.go:313` already has a private
`wordReadingOrderLess`.
- **Rec:** export reusable comparators on `[]page.TextCell` (e.g.
  `page.LessByLeft`, `page.LessByIndex`) for `slices.SortFunc`. Biggest
  line-count reduction in the audit.
- **Risk:** low, but verify stable-sort semantics are preserved where
  `sort.SliceStable` is used (reading order can be sensitive). Do this **after**
  D1–D4 so diffs stay reviewable.
- [ ] **D5.**

### 1f. Whitespace / zero-width-rune normalisation (~7 sites)
`strings.Join(strings.Fields(x)," ")` at `pkg/pdf/assemble.go:411`,
`connect.go:327`, `table/detect.go:2347`, `render/markdown.go:55`. Separately, a
canonical zero-width predicate already exists (`pkg/pdf/text.go:90
isZeroWidthFormatRune`) but is bypassed by `assemble.go:423`, `structure.go:271`,
`render/markdown.go:51`.
- **Rec:** one `CollapseWhitespace` helper; route the zero-width strippers
  through the existing predicate. **Risk:** low — confirm each call site's
  trim/replace pre-steps are preserved. — [ ] **D6.**

### 1g. Axis-overlap helpers → `geom`
`pkg/pdf/readingorder_graph.go:96-113` (`horizontallyOverlaps`,
`verticallyOverlaps`, `rectsIntersect`) and `pkg/table/detect.go:4069`
(`horizontalOverlapRatio`) re-derive intersection geom. `grid.go:487
overlapFractionOfCell` already delegates and is effectively
`IntersectionOverSelf` (`geom.go:105`).
- **Rec:** add epsilon-aware `OverlapX/OverlapY` (the graph variant needs its
  `roEpsilon` tolerance) to `geom`; point `overlapFractionOfCell` at the existing
  method. **Risk:** medium — tolerances matter; smallest item, do last.
- [ ] **D7.**

---

## 2. Delete dead code (verified zero non-test references)

- [ ] **X1.** `pkg/pdf/table_region.go` — `DetectTableRegions` + `TableRegion`
  (255 LOC) are exported but **never used by the pipeline**; kept alive only by
  `table_region_test.go`. Confirmed: grep finds no references outside the file
  and its test. Delete the file + test (or, if you want to keep the algorithm
  parked, move it under a clearly-labelled `internal/` spike). Also clears the
  dead local `_ = h` at `table_region.go:229`.
  <br>**What replaced it (you asked):** `table_region.go` was an early
  *line-zone* table classifier in `pkg/pdf` that was never wired into
  `pageMarkdownBlocks`. The live table detector is the `pkg/table` package —
  `doctable.DetectTables` (`backend.go:132,452`) plus `DetectAnchoredTextTables`
  (`detect.go:481`), backed by the alignment/grid reconstruction in
  `pkg/table/{detect,grid,alignment}.go`. So this isn't a "remove a working
  detector" — it's deleting a superseded prototype that the grid-based detector
  replaced (git: added in `64cefe9` "LineElement pipeline foundation", left
  behind when detection moved into `pkg/table`).
- [ ] **X2.** `pkg/pdf/lineelement.go:460-476` — `cellFontName`, `isBoldFont`,
  `isItalicFont`: zero callers (superseded by `page.TextCell.IsBold/IsItalic`).
  Delete.
- [ ] **X3.** `pkg/pdfium/internal/font/font.go:44` `fontStyleIsAllCaps`: zero
  callers. Delete (audit sibling `fontStyle*` predicates while there — some may
  also be unused).

---

## 3. Idiomatic Go

### 3a. De-stutter primary public types — [ ] **I1.**
- `pkg/table`: `TableCell` → `Cell`, `TableData` → `Data`, `TableValidityScore`
  → `ValidityScore` (`table.go:11,25`, `detect.go:320`). `table.Cell`/
  `table.Data` is the clearest win — these are the package's headline types.
- `pkg/textline`: `TextLineWord` → `Word` (`textline.go:75`). The `pkg/pdf`
  alias (`lineelement.go:21`) stays; only the origin renames.
- `internal/benchdp`: the uniform `Tool*` prefix (`ToolConfig`, `ToolRunner`,
  `ToolBenchmarkResult`, …) reads as `benchdp.ToolConfig`. Soft stutter; worth a
  pass since it's internal-only (zero external blast radius).
- **Risk:** wide but mechanical; gopls rename. `table.TableData` has the most
  call sites — do it as its own commit.

### 3b. Break up the two god-files into sub-packages — [ ] **I2.**
Your steer: *small, well-defined sub-packages beat large files.* Agreed — so the
target is extraction into cohesive packages with a real API boundary, not just
splitting one `package table` across more files.
- `pkg/table/detect.go` — **4349 LOC**. Lift the phases into sub-packages with
  narrow interfaces: e.g. `pkg/table/region` (region-finding), `pkg/table/score`
  (validity scoring), `pkg/table/grid` already exists as a sibling and can absorb
  cell assembly. The public `table.DetectTables` stays the façade.
- `pkg/pdf/headings.go` — **2016 LOC**. Extract a `pkg/pdf/heading` sub-package:
  detection, the false-positive guard stack, and leveling. `pkg/pdf` keeps the
  thin entry (`splitHeadingCellsProtecting`) and calls in.
- **Caveat (why this is more than a file move):** these heuristics lean on many
  *unexported* shared helpers (font-metric, isolation, box-center). A package
  boundary forces those onto a small exported surface (or a shared
  `internal/layout` helper pkg). That's the work — and the payoff, since it makes
  the heuristics testable in isolation. Land the §1 geom/helper consolidation
  (D1–D7) **first** so the shared helpers already have a home before the split.
- **Risk:** low-medium (mechanical moves + deciding the cut-points); no behaviour
  change. Do *before* touching the heuristics in §4.

### 3c. Package docs & exported-symbol docs — [ ] **I3.**
No `// Package …` doc on `pdf`, `table`, `page`, `geom`, `render` (the headline
`pkg/pdf` most visibly). `pkg/geom/geom.go` exported types/methods (`CoordOrigin`,
`Size`, `Box`, `Width/Height/Area/...`) are undocumented; the one existing block
(`geom.go:24`) covers three funcs at once instead of starting with each name.
Add per-symbol docs. **Risk:** none.

### 3d. Tighten the public surface — [ ] **I4.**
- `pkg/pdf/oracle/oracle.go:28` `Initialiser` is an exported **mutable
  package-level func var** (injection hook). **You asked: can we remove the DI
  entirely?** See §5 — yes, and it's the right call; the function-pointer
  registration global exists only to bridge a test-only module boundary and a
  build tag does the same job without mutable global state. Folded into §5.
- `pkg/page` `SegmentedPage`/`CellsInBox` (`page.go:41-66`) — exercised only by
  tests, doc says "mirrors docling-core". Vestigial; unexport or remove.
- `pkg/pdf` re-exports backend-plumbing (`TextRect`, `TextRectsToCells`,
  `TopLeftBoxToPDFiumBounds`, `MergeFragmentedCells`, `AssembleLineElements`)
  purely so the engine can call back in — a bidirectional public coupling.
  Longer-term: demote to an `internal/` helper package. **Risk:** medium (engine
  imports these); sequence *after* the rename. Flagging, not urgent.
- `pkg/geom` `BoxFromTuple`/`AsTuple`/`Scaled` are test-only but documented as a
  deliberate docling-core-mirroring value API — **keep**; noted for awareness.

---

## 4. Fix the literal-text heading heuristic (AGENTS.md §11)

You confirmed: this is a violation and needs to be fixed, and we should look at
how the other benchmark tools solve it. We did — here's the finding and a
concrete, document-general replacement.

### What the competitors actually do (none use a keyword list)
- **Docling** (the strongest structural tool): an **ML layout model** emits a
  `SECTION_HEADER` region label (`.upstream-docling/docling/models/stages/layout/`);
  the literals "abstract"/"introduction" appear in its source **only** in markup
  backends (LaTeX/JATS), never in PDF classification. Levels default to 1.
- **pymupdf4llm / liteparse / opendataloader**: **font-size (and
  weight) clustering** — size bands → `#`/`##`/`###`. No keyword maps.
- **markitdown / pypdf**: barely detect headings at all (text extraction only).

So "copy what they do" = **use font weight + size + layout, never literal text.**

### The real root cause (not what the comment claims)
The in-code comment blames bold-blindness, but the research disproves it for this
corpus. Of the 4 DPBench papers with `# Abstract`/`# Introduction` ground truth,
**3 are bold and the font layer resolves their weight correctly** (e.g.
`NimbusRomNo9L-Medi` → `weight=700, IsBold()=true`, derived from the descriptor
`ForceBold` + embedded Type1 `/Weight`, `pkg/pdfium/internal/font/font.go:479`).
The 4th (non-bold) is caught purely on size (1.14–1.33× body). The actual reason
the keyword crutch exists: those bold headings are only **~1.08× body size** —
*below* `headingScaleFactor = 1.18` (`headings.go:17`), so `fontProminent` is
false. The code **already computes a `nearProminent` floor at 1.08×**
(`headings.go:1087`) **and** has `IsBold()` on every cell — but at the deciding
branch it reads the keyword instead of the bold flag it never consults.

### The fix — `H1` (recommended: **P1**, swap keyword for the bold signal)
Replace the `return isCommonSectionHeading(text)` tail (`headings.go:1092`) with:
promote when the line is **majority-bold** AND (`nearProminent` at 1.08× OR
`isolated`). Implementation: add a `prominentBoldShare(line)` mirroring the
existing `prominentShare` (`headings.go:1745`) but testing `cell.IsBold()` /
`LineElement.Bold` (both already populated, `pkg/page/page.go:94`,
`lineelement.go:272`). ~30 lines, no new font plumbing, no literal text.
- The existing exclusion guards (`looksLikeNumberedAcronymTableRow`,
  table-caption, running-header) still run *before* this tail — that's the fence
  that neutralises the table-header-row regression an earlier isolation-only
  weight experiment hit (−0.012 TEDS). Here the bold signal is gated by isolation
  **plus** the full guard stack.
- **Levels need no change**: DPBench ground truth is 100% level-1 headings and
  Docling itself emits PDF headings at level 1, so MHS scores heading
  *presence/text*, not depth. Leave `assignDocumentHeadingLevels` (decimal
  numbering) untouched.
- Same pass can drop the other literal-word *inclusion* signals
  (`appendixLetterHeadingPattern` `headings.go:32`, `partSectionMarkerPattern`
  `:43`). The *exclusion* guards (`isNumberedFigureCaptionText`,
  `startsTableCaptionCue`) are lower-risk false-positive fences — defensible to
  keep, but note them as residual debt.

### Non-negotiable: measure it
This is the one change in this whole review that alters extraction output, so it
is **its own PR**, not part of the mechanical cleanup, and it runs the AGENTS.md
200-PDF before/after protocol. Gate: `table_structure_teds` ≥ current AND
`heading_level_mhs` ≥ the keyword baseline. Only 4 corpus files carry these
headings, so MHS is swingy — report per-document heading diffs on those 4 plus
the full-200 TEDS delta before deleting `commonSectionHeadings`.

- [ ] **H1.** Implement P1 (bold + isolation replaces `commonSectionHeadings`),
  delete the keyword map + the two literal inclusion patterns, validate per
  above. Separate PR.

---

## 5. Retire porting-era scaffolding (the "wasm" naming + oracle DI)

Both of your comments here point at the same thing: validation plumbing from when
the native engine was being *built against* real PDFium. Worth separating fact
from naming.

### 5a. "wasm" is stale naming, not a live runtime — purge it — [ ] **W1**
**There is no WebAssembly/wazero in the tree.** Grep confirms zero `wazero` /
`webassembly` imports anywhere; the oracle is **cgo** (klippa `go-pdfium`
`single_threaded`, in-process cgo — `pdfium-oracle/oracle.go:16`). The "wasm"
vocabulary is a fossil from an earlier architecture where the oracle was PDFium
compiled to wasm. So "drop wasm support" is **not** removing a runtime (there
isn't one) — it's deleting misleading words that now actively lie:
- `difftest.BootWASM` → rename `BootOracle` (the real fn is already named that at
  `difftest.go:91`; `BootWASM` is a stale alias used by `diff_test.go:51`,
  `dumpcells_test.go:28`, `measure_test.go:32`).
- ~15 "wasm oracle" / "the wasm backend IS PDFium" comments across
  `pkg/pdfium/{backend.go,diff_test.go,measure_test.go,internal/...}` — reword to
  "cgo oracle".
- The telemetry backend label `"wasm"` (`pdfium-oracle/internal/pdfiumcore/backend.go:37`)
  and the README "wasm-vs-cgo-mt comparison" — there's only one oracle backend
  now (cgo); drop the dead label/comparison.
- The `difftest.go:198` skip-needle list (`"wasm","wazero","instantiate",…`) can
  shed the wasm/wazero entries.
- **Risk:** none — comments, one fn rename, one metric label. Pure hygiene.
- **Note:** this does **not** let us drop the float32/no-FMA `crt` code — that
  matches PDFium's C++ `float` math (cgo or wasm, same math). Simplifying *that*
  only becomes possible under 5b-option-O2.

### 5b. Remove the oracle dependency-injection global
`pkg/pdf/oracle` is a registration seam: a mutable global `Initialiser` func var
(`oracle.go:28`) + `SetInitialiser` + `New()`. It's imported by exactly **two**
files: `difftest.go` (consumer) and `pdfium-oracle/oracle.go` (registers the cgo
backend at link time). Its only job is to let the *test* harness reach the cgo
oracle that lives in a separate module, while keeping the product from ever
linking klippa. You asked "do we really need DI — can we remove it entirely?".
Two ways, depending on how much of the validation apparatus you want to keep:

- **O1 (recommended): kill the global, keep the differential tests.** Replace the
  runtime-registered func var with a **build-tagged constructor**: a
  `//go:build pdfium_cgo` file that directly constructs the cgo backend (importing
  `pdfium-oracle`), and a default-build stub returning `ErrOracleUnavailable`.
  Same module isolation (product still never links cgo), but no mutable global
  state and no registration dance — the dependency is resolved by the compiler,
  not at startup. `pkg/pdf/oracle` collapses to the sentinel error + a
  `New()` that's build-tag-selected. **Risk: low.** This is the literal answer to
  "remove the DI" without losing the engine's regression guard.
- **O2 (bigger call): retire the whole oracle + the differential suite.** If you
  consider the native port validated and done, delete `pkg/pdf/oracle`, the
  `internal/difftest` package, the `diff`/`measure`/`dumpcells` tests, and the
  `pdfium-oracle/` module outright. This is the only path that *also* unlocks
  simplifying the float32/no-FMA `crt` gymnastics (§1 preamble) — without an
  oracle to match bit-for-bit, `crt` can use ordinary float math. **Risk: real**
  — you lose the only test that proves the native engine matches PDFium, and the
  `crt` simplification is a separate, carefully-validated effort against the
  DPBench corpus. Defensible if the port is "frozen", but it's a strategy
  decision, not a cleanup.

- [ ] **O1/O2.** Pick: kill the DI global but keep diff tests (O1), or retire the
  oracle apparatus entirely (O2). Recommend **O1** unless you're ready to declare
  the port frozen.

---

## Suggested sequencing (low-risk → higher)

1. **W1** purge stale "wasm" naming (pure hygiene, zero risk).
2. **X1–X3** delete dead code (zero risk, shrinks the diff surface first).
3. **I3** package/symbol docs.
4. **D1–D4** geom/abs/min-max/roundf consolidations (mechanical, `go vet` gates).
5. **R1** the rename `pkg/pdfium` → `pkg/parser` (its own commit; gopls-driven).
6. **I1** de-stutter (`table.Data`/`Cell` is the big one; own commit).
7. **D5–D7** comparator/normalisation/overlap consolidation (sort-stability +
   tolerance review).
8. **I2** sub-package extraction (after D1–D7 so shared helpers have a home).
9. **O1** kill the oracle DI global (or **O2** if retiring the apparatus).
10. **I4** surface-tightening.
11. **H1** the heading §11 fix — **separate PR, measured against DPBench.**

Groups 1–9 are independently shippable and **output-neutral** — verifiable with
`go vet ./...` + the existing suite, plus one DPBench parity run to prove
byte-identical output. **H1 is the only behaviour change** and carries its own
before/after validation. **O2** (if chosen) is a strategy call that unlocks the
later `crt` simplification.
