# TeX Font Encoding Support Implementation Plan

**Goal:** Resolve Unicode for glyphs in TeX-produced fonts whose char codes follow
the Computer Modern encodings rather than ASCII, so that minus signs, Greek
letters, relations, integrals and radicals stop being dropped or mis-mapped.

**Why this matters:** A four-way visual audit of `entropy.pdf` (55 pages)
attributed ~1530 of ~2550 defect sites to this one cause — 60% of everything
wrong with the document. It is the only defect class that produces output a
reader will *believe*: `H = -K∑p log p` renders as `H = K∑p log p`, and
`H(x,y) ≤ H(x)+H(y)` renders as `H (x; y)  H (x)+ H (y)`. Poppler's
`pdftotext` fails identically, so this is a chance to beat the reference
implementation, not merely match it.

**Scope boundary (read this first):** This plan covers UNICODE RESOLUTION ONLY.
Do not attempt 2-D math layout, LaTeX emission, or fraction reconstruction —
those are separate plans and depend on this one landing first.

**Tech Stack:** Go, `pkg/parser/internal/font`, Testify.

---

## Background: the exact mechanism

`entropy.pdf` embeds 24 Type 3 fonts produced by dvips from Computer Modern
bitmap fonts. Verified facts about them:

- **No `/ToUnicode` CMap on any font** (all 24 Type 3, all 3 Type 1).
- **Glyph names are synthetic.** dvips writes `/Encoding /Differences
  [0/, /#2301 /#2302 12/#230C 16/#2310 ...]`. The name `#2314` decodes (PDF `#`
  hex escape) to the literal name `#14` — the char code in hex. These names carry
  no semantic content and must not be trusted. Note also that code 0 is named
  `,` (a comma), which is why poppler prints a comma where the minus belongs.
- **Char codes follow the TeX font encodings**, not ASCII.

The relevant slot mappings, confirmed against the rendered pages:

| Font | Slot | Glyph | We currently emit |
| --- | --- | --- | --- |
| cmsy | 0x00 | − minus | nothing (NUL) |
| cmsy | 0x06 | ± | nothing |
| cmsy | 0x14 / 0x15 | ≤ / ≥ | invisible control bytes |
| cmsy | 0x18 / 0x19 | ∼ / ≈ | invisible control bytes |
| cmsy | 0x1C / 0x1D | ≪ / ≫ | invisible control bytes |
| cmsy | 0x21 | → | `!` |
| cmsy | 0x30 | ′ prime | `0` |
| cmsy | 0x6A | \| bar | `j` |
| cmsy | 0x70 | √ radical | `p` |
| cmmi | 0x0B–0x1A | α β γ δ ε ζ η θ ι κ λ μ ν ξ π ρ | invisible control bytes |
| cmmi | 0x21 | ω | `!` |
| cmmi | 0x3A | math period | `:` |
| cmmi | 0x3B | math comma | `;` |
| cmmi | 0x3D | solidus `/` | `=` |
| cmmi | 0x60 | ℓ | `` ` `` |
| cmex | 0x5A | ∫ | `Z` |
| cmex | 0x68 / 0x69 | big brackets | `h` / `i` |

The single cleanest proof: cmmi slot 0x21 is ω and cmsy slot 0x21 is →. Both
emit `!` today. Same slot, different fonts, same wrong output — that is a
fallback table, not a layout bug.

---

### Task 1: Establish the failing baseline

**Files:**
- Test: `pkg/parser/internal/font/texencoding_test.go` (new)

1. Write a test that loads a synthetic Type 3 font dictionary shaped like the
   dvips output above (FontMatrix `[1 0 0 -1 0 0]`, `/Differences` with `#XX`
   hex-form names, no `/ToUnicode`) and asserts `UnicodeFromCharCode(0x14)`
   returns `≤`.
2. Run it. Confirm it fails, returning 0 or a control character.
3. Keep this test — it is the acceptance criterion for Task 4.

### Task 2: Add the TeX encoding tables

**Files:**
- Create: `pkg/parser/internal/font/texencoding_data.go`
- Create: `pkg/parser/internal/font/texencoding.go`

1. Encode the four standard TeX encodings as `[256]rune` tables: `cmr` (OT1
   text), `cmmi` (math italic), `cmsy` (math symbols), `cmex` (math extension).
   Source them from the published TeX font encoding definitions — these are
   stable, standard, and documented; do not invent slots. Where a slot has no
   sensible Unicode (e.g. cmex delimiter pieces), leave it 0 so existing
   fallbacks apply.
2. Mark the file `// Code generated ... DO NOT EDIT.` in the style of
   `fontmapper_data.go`.
3. Add unit tests asserting a handful of known slots per table (cmsy 0x00 = −,
   cmmi 0x0B = α, cmex 0x5A = ∫). Include at least one NEGATIVE assertion that
   an unassigned slot is 0.

### Task 3: Identify which TeX encoding a font uses

**Files:**
- Modify: `pkg/parser/internal/font/texencoding.go`
- Test: `pkg/parser/internal/font/texencoding_test.go`

This is the hard part and the part most likely to be got wrong. **Read
`AGENTS.md` before designing it.** Detection must be algorithmic, deterministic
and document-general; font metrics are an explicitly endorsed signal, literal
document text is forbidden.

Recommended approach — **normalised width-vector fingerprint**:

1. Embed the published TFM advance widths for `cmr10`, `cmmi10`, `cmsy10` and
   `cmex10` (1000-unit design-size values).
2. For a candidate font, take its `/Widths`, restricted to the codes actually
   present, and normalise (e.g. divide by the vector's max) so the comparison is
   scale-free. dvips Type 3 widths are bitmap device units, so only ratios are
   meaningful — an absolute comparison will fail.
3. Score each candidate encoding by mean absolute error over the shared codes.
   Accept the best match only if (a) it clears an absolute error threshold AND
   (b) it beats the runner-up by a clear margin. **When ambiguous, return no
   match and leave existing behaviour untouched.** A wrong encoding is worse
   than no encoding.
4. Require a minimum number of overlapping codes (suggest ≥4) before trusting a
   fingerprint at all; a 1-glyph subset font cannot be identified.

**Gate the whole mechanism** so it can only apply when the evidence says the
document is TeX-produced and nothing better is available:
- the font has NO `/ToUnicode` (an explicit map always wins), AND
- the font's glyph names are absent or synthetic (all matching `^#[0-9A-Fa-f]{2}$`).

Write BOTH positive and negative tests: a cmsy-shaped width vector identifies as
cmsy; a font with a `/ToUnicode` map is left alone; a font with real Adobe glyph
names is left alone; an ambiguous width vector returns no match.

### Task 4: Wire resolution into the font loader

**Files:**
- Modify: `pkg/parser/internal/font/font.go` (`UnicodeFromCharCode`)
- Modify: `pkg/parser/internal/font/load.go`

1. Resolve the encoding once at load time, cache the chosen table on the `Font`.
2. In `UnicodeFromCharCode`, apply the TeX table ONLY as a fallback — after
   `/ToUnicode` and after the existing encoding path, and only when those yield
   0 or a control character (< 0x20). Never override a successful mapping.
3. Make Task 1's test pass.
4. Run `go test ./pkg/parser/...`.

### Task 5: Verify on the real document

**Files:**
- Generate: `/tmp/entropy-texenc.md`

1. `go build -o bin/docmill-bench ./cmd/docmill && ./bin/docmill-bench convert entropy.pdf > /tmp/entropy-texenc.md`
2. Confirm these all become non-zero (they are all zero today):
   minus signs `−`, Greek `αβγδεηθλμνπρστφω`, relations `≤≥`, integral `∫`,
   radical `√`.
3. Spot-check three specific known-broken strings from the audit:
   - `H = ∑pi log pi` should regain its leading minus.
   - `C = W log(1 + P/N)` on page 45 should contain a `/` not `+ N`.
   - Table I on page 40 should show `-8.69`, not `8:69`.
4. Confirm the prose is NOT regressed: page 1's opening paragraph must still read
   correctly in plain English.

### Task 6: Validate against the corpus

**Files:**
- Generate: `benchmarks/dpbench/results/*.json`

1. Follow the DPBench protocol in `AGENTS.md` exactly.
2. **Run the benchmark with nothing else running on the machine.** Measured
   run-to-run variance on the subprocess harness is ±3 ms/page under load; a
   latency conclusion drawn while other agents are working is invalid. Prefer an
   in-process A/B for latency claims.
3. Required outcome: `errors` stays 0, and no accuracy metric regresses.
   `extraction_accuracy` should IMPROVE on any TeX-produced document in the
   corpus. Record all deltas.
