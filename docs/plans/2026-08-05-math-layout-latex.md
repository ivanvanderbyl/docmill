# 2-D Math Layout and LaTeX Emission Implementation Plan

**Goal:** Reconstruct two-dimensional mathematical layout (fractions,
sub/superscripts, big-operator limits, radicals, delimiters) and emit it as
LaTeX-style Markdown math, so display equations survive as mathematics rather
than as scattered tokens.

**DEPENDENCY — read this first:** This plan REQUIRES
`2026-08-05-tex-font-encodings.md` to have landed. There is no value in emitting
well-formed LaTeX around the wrong symbols: today a "fraction" would be built out
of a minus sign that does not exist and Greek letters that render as control
bytes. Do not start this plan until the encoding work is merged and its
verification step passes.

It also overlaps `2026-08-05-display-equation-reading-order.md`: that plan
isolates a math band from surrounding prose; this plan interprets the band's
internal structure. Land reading order first, or coordinate closely.

**Why this matters:** There are currently ZERO `$` characters in the 55-page
output. Every fraction is flattened to juxtaposition, which inverts meaning:
`log p(x,y)/p(x)` becomes `log p(x,y) p(x)`, a product. `1/2` becomes `12`.
`2^{HN}` and `2^{-HN}` — reciprocals — both render as `2HN`. `C = W log(1 + P/N)`
renders as `P C = W log 1 ch + N : n`.

**Tech Stack:** Go, new `pkg/pdf` math module, Testify.

---

### Task 1: Design the intermediate representation

**Files:**
- Create: `pkg/pdf/mathlayout.go`

1. Define a small tree: `Row`, `Frac{num, den}`, `Script{base, sup, sub}`,
   `BigOp{op, lower, upper, body}`, `Radical{body}`, `Delimited{open, body, close}`,
   `Atom{text}`.
2. Keep it minimal. Do not attempt full MathML/TeX coverage — the target is the
   constructs that actually appear in scientific papers.

### Task 2: Detect structure geometrically

**Files:**
- Modify: `pkg/pdf/mathlayout.go`
- Test: `pkg/pdf/mathlayout_test.go`

Constraint from `AGENTS.md`: geometry and font metrics, not character content.

- **Fraction:** a horizontal rule segment (already available via
  `RulingSegments`) with cells above and below, horizontally overlapping it.
  This is the most reliable signal available and should be built first.
- **Superscript / subscript:** a cell whose font size is materially smaller than
  the run's dominant size AND whose baseline is offset up (sup) or down (sub)
  relative to the preceding base cell.
- **Big-operator limits:** cells centred above/below a tall glyph, where the
  tall glyph's height materially exceeds the line's dominant cell height.
- **Radical / delimiters:** identified from glyph extent and the encoding work's
  output; a radical spans its body horizontally.

Write a test per construct, each built from realistic geometry captured from
`entropy.pdf`, plus a NEGATIVE test that ordinary prose with a footnote marker is
not mistaken for a superscript expression.

### Task 3: Serialise to LaTeX

**Files:**
- Modify: `pkg/pdf/mathlayout.go`
- Test: `pkg/pdf/mathlayout_test.go`

1. Render the tree: `\frac{}{}`, `^{}`, `_{}`, `\sum_{}^{}`, `\sqrt{}`,
   `\left( \right)`.
2. Inline math wrapped in `$...$`; display math on its own line in `$$...$$`.
3. Escape correctly and keep output deterministic.
4. Map the Unicode operators the encoding plan now produces to their LaTeX
   commands where a command is clearer (`≤` → `\le`, `∫` → `\int`, `∑` → `\sum`).

### Task 4: Integrate behind an option

**Files:**
- Modify: `pkg/pdf/backend.go` (`ExtractionOptions`)

1. Add `EmitMath bool` to `ExtractionOptions`, defaulting OFF.
2. Wire it so that with the option off, output is byte-identical to today. Add a
   test asserting that.
3. Enable it in `ExtractMarkdown` only once Task 6 shows no corpus regression.

### Task 5: Verify on the real document

1. Rebuild and reconvert `entropy.pdf` with the option on.
2. Check these specific results are recoverable and correct:
   - `H = -K\sum_{i=1}^{n} p_i \log p_i` (page 11)
   - `C = W\log\left(1 + \frac{P}{N}\right)` (page 45)
   - `C = \lim_{T\to\infty}\frac{\log N(T)}{T}` (page 3)
   - `H(x,y) \le H(x) + H(y)` (page 12)
3. Confirm prose is untouched: paragraphs containing no mathematics must be
   byte-identical to the option-off output.

### Task 6: Validate against the corpus

1. Follow the DPBench protocol in `AGENTS.md`.
2. Be aware the ground truth may not contain LaTeX, so `extraction_accuracy`
   could FALL even though the output is better. If that happens, document the
   trade-off explicitly and keep the option OFF by default — `AGENTS.md` requires
   a regression to be explicit, measured and accepted, not silently shipped.
3. Required: `errors` stays 0.
4. Run with nothing else on the machine; variance under load is ±3 ms/page.
