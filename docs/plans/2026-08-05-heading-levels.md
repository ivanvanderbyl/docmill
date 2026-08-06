# Heading Level Hierarchy Implementation Plan

**Goal:** Emit a real heading hierarchy (`#`, `##`, `###`) instead of flattening
every heading in a document to `#`.

**Why this matters:** `entropy.pdf` currently emits 72 headings, all `#`. The
paper title, `PART I: DISCRETE NOISELESS SYSTEMS`, `1. THE DISCRETE NOISELESS
CHANNEL` and `APPENDIX 3`'s subtitle `THEOREMS ON ERGODIC SOURCES` are all
siblings. Firecrawl's pdf-inspector does emit levels on this document, and it is
the one structural dimension where it clearly beats us. DPBench's
`heading_level_mhs` metric measures exactly this, so there is a corpus-wide
score to move as well as a readability win.

**Current state:** `assignHeadingLevels` and `assignDocumentHeadingLevels`
already exist in `pkg/pdf/headings.go` and `pkg/pdf/backend.go`. Read them
first — the machinery is partly there and this may be a repair rather than a
new build. Establish empirically WHY every heading currently collapses to level
1 before designing a fix.

**Tech Stack:** Go, `pkg/pdf/headings.go`, Testify.

---

### Task 1: Diagnose the collapse

**Files:**
- Read: `pkg/pdf/headings.go` (`assignHeadingLevels`, `headingLine.level`)
- Read: `pkg/pdf/backend.go` (`assignDocumentHeadingLevels`)

1. Determine why every heading ends up level 1 on `entropy.pdf`. Instrument if
   necessary.
2. Write down the finding before changing code. Likely candidates: levels are
   assigned per page and the document-level pass cannot see enough structure; or
   the font-size buckets collapse because the headings genuinely share a point
   size; or the numbering hierarchy is not being consulted.

### Task 2: Write failing tests

**Files:**
- Test: `pkg/pdf/headings_internal_test.go`

1. Build a synthetic multi-page document with: a large-font title, several
   `PART N: ...` headings at one size, numbered `N. TITLE` sections at a smaller
   size, and an appendix subtitle.
2. Assert the expected levels: title 1, PART 2, numbered section 3, subtitle
   under its appendix parent.
3. Run and confirm failure.

### Task 3: Implement level assignment

**Files:**
- Modify: `pkg/pdf/headings.go`

Design constraints from `AGENTS.md`: signals must be document-general. Font
metrics and numbering STRUCTURE are permitted; matching literal section names is
not. Note that `headings.go` carries explicit comments recording that a
`commonSectionHeadings` literal-word map was previously removed for exactly this
reason — do not reintroduce anything of that shape.

Permitted signals, in rough order of reliability:
1. **Numbering depth.** `2.1.3` is deeper than `2.1`, which is deeper than `2`.
   This is structural, not lexical.
2. **Font metric bands.** Cluster heading font sizes into bands; larger band =
   shallower level. Must be relative to the document's own body metric, not
   absolute point sizes.
3. **Containment.** A heading appearing between two headings of a shallower
   level is a child of the preceding one.

Rules:
- Levels must be assigned across the WHOLE document, not per page — a numbered
  section on a page that happens to lack its parent must still nest correctly.
- Cap depth at 6 (Markdown's limit).
- Never produce a level jump greater than 1 from the preceding heading
  (`#` then `###` is malformed); clamp instead.

### Task 4: Verify on the real document

1. Rebuild and reconvert `entropy.pdf`.
2. Expect roughly: the title `#`; `PART I`–`PART V` and `APPENDIX 1`–`7` at one
   level; the 28 numbered sections one level deeper; appendix subtitles
   (`THEOREMS ON ERGODIC SOURCES`, `MAXIMIZING THE RATE FOR A SYSTEM OF
   CONSTRAINTS`, `NUMBER OF FINITE STATE CONDITION`) nested under their appendix.
3. Confirm the heading COUNT is unchanged (currently 72) — this plan changes
   levels only. If the count moves, something else regressed.

### Task 5: Validate against the corpus

1. Follow the DPBench protocol in `AGENTS.md`.
2. `heading_level_mhs` is the primary metric and must improve on the current
   0.770814.
3. Required: `errors` stays 0 and no other metric regresses.
4. Run with nothing else on the machine; variance under load is ±3 ms/page.
