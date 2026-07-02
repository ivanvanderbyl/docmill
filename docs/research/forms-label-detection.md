# Form field label detection from digital data only

Research notes for associating a human-readable label with each AcroForm
field (or group of fields) using only machine-readable PDF data: widget
rectangles, positioned text runs, font attributes, and document/AcroForm
dictionaries. No OCR, no rendering, no pixel or LLM analysis.

Test document: `KS2-empty.pdf` (NZ Inland Revenue KiwiSaver deduction form,
April 2026 revision) — a deliberately hard case that mixes at least five label
conventions on one page (see §4).

## 1. Signal inventory, ranked

Digital PDFs carry several label signals that need no geometry at all. A
production algorithm should be a **cascade**: use the authored signals when
present, fall back to geometry only for the gaps.

| Rank | Signal | Where | Reliability |
|---|---|---|---|
| 1 | `/TU` alternate field name ("tooltip") | field dict (PDF 32000-1 §12.7.3.1, Table 220) | Authoritative when present. PDF/UA-1 (ISO 14289-1) *requires* it for accessible forms; Acrobat's accessibility checker flags fields missing it. Well-authored government forms usually have it. |
| 2 | Tagged structure tree | `Form` struct elem wrapping the widget's `OBJR`, its `/Alt`, or an adjacent `/Lbl` struct elem | Authoritative when the PDF is tagged (PDF/UA); rare in the wild outside government/enterprise output. |
| 3 | Checkbox/radio **on-state name** | widget `/AP /N` state dict key ≠ `Off`; also radio `/Opt` export values | Frequently encodes the option caption (`/Yes`, `/Mr`, `/3.5%`) because authors name states after options. Free of geometry; verify against nearby text before trusting blindly (authors also leave `/1`, `/Check Box3`). |
| 4 | `/T` field name | field dict chain (fully-qualified name) | Semantic hints only ("ird_number_1", "applicant.firstName"). Needs splitting (dots, underscores, camelCase) and is often machine junk ("Text42"). Good tie-breaker, bad primary source. |
| 5 | Geometric text association | page text cells vs widget rects | The research core — see §2–3. Always available for born-digital forms. |
| 6 | Ruling lines / box graphics | content stream paths (`Page.RulingSegments` already exposes these) | Two uses: segmenting flattened forms (out of scope), and detecting comb/box-row field styling — a widget whose rect is subdivided by vertical rules is a boxed character field, which strongly predicts the below-caption convention. |

### 1.1 Spec notes

- **`/TU`** (PDF 32000-1 §12.7.3.1 Table 220): "an alternate field name that
  shall be used in place of the actual field name wherever the field shall
  be identified in the user interface … also useful when extracting the
  document's contents in support of accessibility". **Not inheritable**:
  Table 220 marks only FT/Ff/V/DV inheritable (PDFium and this parser still
  walk the parent chain, which is fine for merged widget/field dicts).
  Matterhorn checkpoints **28-005** (field has neither `/TU` nor an `/Alt`
  on the enclosing struct elem) and **28-010** (widget not nested in a
  `Form` tag) are what PDF/UA validators enforce. Reality check: only
  **~44.6%** of interactive PDFs had correct, meaningful `/TU` tooltips in
  the Uckun et al. survey (see §2.3) — the cascade's top rung is empty more
  than half the time.
- **On-state names**: a checkbox (§12.7.4.2.3) / radio (§12.7.4.2.4)
  widget's `/AP /N` is a dictionary with one appearance stream per state;
  the non-`Off` key is the state the widget turns on ("Yes *should* be used"
  per spec — hence generic `A`–`E` states in the wild). Radio/checkbox
  fields may also carry `/Opt` (Table 227, **inheritable**): per-kid export
  strings that are the better label source when present.
- **Structure tree**: in a tagged PDF each widget is referenced by an `OBJR`
  inside a `Form` struct elem; the label is the `Form` elem's `/Alt`, or a
  sibling/preceding `Lbl` struct elem's content. PDF/UA-1 §7.18.1 requires
  one of these. Parsing this is pure dictionary-walking — no geometry — but
  KS2-style legacy forms are often untagged, so it's a bonus path, not the
  core.
- **Comb fields**: text fields with the comb flag (`/Ff` bit 25, value
  0x1000000) and `/MaxLen` are boxed character rows. On government forms the
  comb style correlates strongly with the caption-below convention, so the
  flag is a cheap style prior for direction weighting — no geometry needed to
  detect it.

## 2. Prior art

### 2.1 Web-form label association (transfers directly)

The best-evaluated label-association algorithms come from the hidden-web /
query-interface literature. HTML differs from PDF, but these systems worked on
*rendered geometry* (element bounding boxes), so their rules transfer to
AcroForm widgets almost verbatim.

- **HiWE / LITE** (Raghavan & Garcia-Molina, *Crawling the Hidden Web*,
  [VLDB 2001](http://www.vldb.org/conf/2001/P129.pdf);
  [Stanford TR 2000-36](http://ilpubs.stanford.edu:8090/725/1/2001-19.pdf)).
  Take at most **four candidates — the nearest text piece in each
  direction** (centre-to-centre pixel distance; group centre for widget
  groups); prune candidates **> 6 words**; if any candidate is left or
  above, **drop right/below entirely**; tie-break on bold/larger font.
  **93%** label accuracy on 460 elements vs 72–83% for the Kaljuvee et al.
  WWW 2001 baselines (whose "used-up" rule — a text chunk claimed as a group
  label is excluded as a per-widget label — is the earliest published
  exclusivity constraint).
- **LabelEx** (Hoa Nguyen, Thanh Nguyen, Juliana Freire, *Learning to
  Extract Form Labels*, [PVLDB 1(1) 2008](http://www.vldb.org/pvldb/vol1/1453931.pdf)).
  Candidate window = box of (2·3+1)w × (2·5+1)h centred on the element;
  Naïve-Bayes pruner + J48 decision-tree selector; features: element type,
  bold/italic, placement, alignment booleans, normalised-LCS string
  similarity to the internal name, **density-normalised distance** — their
  published **negative result** is that raw pixel distance "was ineffective";
  normalise by row/column pitch. A reconciliation pass over domain term
  frequencies adds +2.1–8.9% F. Final F 0.86–0.95; label placement is
  domain-dependent (Books corpus: **48% of labels to the right**).
- **OPAL** (Furche, Gottlob, Grasso, Guo, Orsi, Schallhart;
  [WWW 2012](https://dl.acm.org/doi/10.1145/2187836.2187948);
  extended as [*The Ontological Key*, VLDB J. 22 (2013)](https://arxiv.org/pdf/1210.5980)).
  Three scopes with strict precedence and measured false-positive rates:
  **field scope** (explicit associations, 0.3% FP), **segment scope**
  (interleaving/counting: alternate text groups and unlabelled fields in
  order; `|labels| = |fields|` → map 1:1, `|labels| = |fields|+1` → leading
  text is the **group label**; 1.9% FP), **layout scope** (w/nw/n region
  geometry with an *overshadowing* rule — reject text with another field in
  between; **11.1% FP**). Structure before geometry is the headline:
  geometry is the least reliable tier and runs last. Overall **F 96.3%** on
  ICQ/TEL-8.
- **Dragut, Kabisch, Yu, Leser** (*hierarchical schema trees*,
  [PVLDB 2(1) 2009](https://vldb.org/pvldb/vol2/vldb09-360.pdf)). Survey-
  validated design rules: **R2 — a label denotes a field or a group, never
  both**; **R6 — field label side priority left/above/right/below, group
  labels above-or-left only**; **R7 — group members carry labels on the same
  side** (the style-consistency intuition). **Sector model**: the 4 diagonal
  corner sectors around a field are blind spots (never labels); axis sectors
  extend until another field's box. Leaf-label accuracy **92%** on ICQ/WISE.
- **Uckun, Aydin, Ashok, Ramakrishnan**
  ([PACM EICS 2020](https://pmc.ncbi.nlm.nih.gov/articles/PMC8320357/)) —
  the closest published analogue to this parser's setting (PDF text runs +
  widget rects, for accessibility overlays): candidates from 4-direction
  neighbourhoods, ranked preferring **shorter text and smaller distance**;
  F1 0.811 on 100 annotated PDF forms; source of the ~44.6% meaningful-/TU
  statistic.

### 2.2 Browser autofill heuristics (DOM order, not geometry)

Chromium runs two phases
([form_autofill_util.cc](https://source.chromium.org/chromium/chromium/src/+/main:components/autofill/content/renderer/form_autofill_util.cc)):
explicit `<label>` association first (`MatchLabelsAndFields`), then
`InferLabelForElement()` **only if still unlabelled**, first-match: for
**checkable elements `InferLabelFromNext()`** (following text — the
right-caption inversion), then `InferLabelFromPrevious()`, placeholder,
overlaying-successor, `InferLabelFromAriaLabel()` (aria-labelledby beats
aria-label), `InferLabelFromAncestors()` (LABEL, DIV, TD→same colspan
interval in the previous row, DD→previous DT, LI), finally
`InferLabelFromValueAttribute()`. Transplantable details:

- **Validity filter**: labels made only of whitespace/`+*:-–()/.—−－` are
  rejected so a bare `*` or `:` falls through to the next strategy.
- **Overlay rule**: text overlaying the input counts as a placeholder label
  only if it does **not extend below the input's bottom edge** — "that place
  is often used to indicate incorrect inputs" (below-widget overlap = help/
  error text, not label).
- **Text harvest stops at any other form control** (a neighbour's content
  can never leak into a label) — the DOM version of our occlusion test.
- **Shared-label splitting** (proven, later removed from main;
  [M120 label_processing_util.cc](https://chromium.googlesource.com/chromium/src/+/refs/tags/120.0.6099.5/components/autofill/core/browser/form_processing/label_processing_util.cc)):
  2–3 consecutive fields sharing one ≤40-char label split it on `/ , & -`
  and "and" when component count equals field count ("City/State/Zip").
- **Provenance**: every label carries a `LabelSource` enum value (kLabelTag,
  kPTag, kTdTag, kPlaceHolder, kAriaLabel, kValue, kOverlayingLabel, …) — a
  ready-made taxonomy for a `LabelSource` field in Go.

Firefox, by contrast, does **no geometric inference at all**: explicit
labels, document-order adjacency with a 10-node budget, and regex
classification ([LabelUtils.sys.mjs](https://searchfox.org/mozilla-central/source/toolkit/components/formautofill/shared/LabelUtils.sys.mjs)).

### 2.3 PDF world

- **Acrobat form field recognition** — the most explicit published industry
  rule set, from Adobe's
  [*Designing forms for auto field detection*](https://acrobatusers.com/assets/collections/tutorials/legacy/id_2263/acro9_designforms.pdf)
  whitepaper. Direction table: underline fill-in → label **left or below**;
  text box → **inside, left, or above**; check box → **right**; radio group
  → group label **left/above the group**, per-button labels **right of each
  button**; comb field → **above, below, or left**; table cell → cell
  contents or row/column headers. Also documented: "**do not use the same
  text label across multiple fields**" (exclusivity as an authoring rule),
  label font 10–24pt, the word "Signature" near a line ⇒ signature field.
  The adjacent text is written into both the field name and `/TU`. Measured
  recall of Acrobat-style detection on court forms:
  [62–69% of fields](https://arxiv.org/pdf/2312.09198).
- **FormFyxer** (Suffolk LIT Lab,
  [pdf_wrangling.py](https://github.com/SuffolkLITLab/FormFyxer/blob/main/formfyxer/pdf_wrangling.py))
  — the only open-source geometric PDF label associator found: candidate
  text boxes within the field bbox **dilated 50pt**; underscore-run filter;
  winner by an edge-alignment distance preferring left-adjacent/directly-
  above; labels deduplicated across fields (exclusivity again).
- **Accessibility remediation tools**: CommonLook is user-mediated (select
  text + field, click); axesPDF treats tooltips as a manual check; PDFix's
  AddTags writes a **generic** tooltip when `/TU` is missing, not derived
  from adjacent text; PAVE (ZHAW, not ETH) validates/repairs tagging but
  published **no form-label auto-derivation** — a negative result: nobody
  ships the geometric associator these workflows need.
- **PDF.js / PDFBox / pypdf / PyMuPDF / pdfplumber / pikepdf**: all surface
  `/TU` only (source-verified; MuPDF's `pdf_field_label()` falls back to
  `/T`). None attempt geometric label detection.
- **iText pdf2Data**: template-based (human draws the label/data zones), not
  automatic; discontinued 2024.
- **IBM Docling**: **no AcroForm extraction at all**; `form` regions come
  from the vision layout model and are "currently ignored in downstream
  processing". docling-core defines the target schema (`FormItem`,
  KEY/VALUE graph cells) but nothing populates it from PDF widgets — the
  gap this work fills for docmill.

### 2.4 What the folklore gets wrong

The naive "nearest text left, else above" rule embedded in most tools fails
on three patterns that real (especially government) forms use heavily:

1. **Caption *below* the box** — IRD/HMRC/ATO style: a row of comb boxes with
   the caption in small type underneath ("First names", "Surname",
   "Postcode"). Left/above preference picks the *previous* row's caption —
   systematically wrong for the whole form.
2. **Option captions to the right** — checkboxes/radios ("Yes", "Mr",
   "3.5%"). Needs a per-type direction inversion.
3. **Group labels** — "5. Your name" labels a *group* (title checkboxes +
   first-names row + surname row); each member also has its own sub-caption.
   A flat field→string mapping loses the question context that makes labels
   unambiguous ("Day" alone vs "Your contact numbers — Day").

## 3. Candidate algorithms

> **Status**: the winning algorithm (D) shipped as `pkg/forms` (entry point
> `forms.Detect`) and is wired into `docmill forms layout`, which emits
> `label`, `labelSource`, `groupLabel`, and `onState` per field, plus a
> per-page `groups` array normalising the multi-widget structure — kinds
> `question` (numbered anchor bands), `field` (widgets sharing one /T, e.g.
> a Yes/No checkbox pair), and `cluster` (adjacent widgets sharing one
> caption), members referenced by page-local field index (names repeat, so
> indices are the stable key). Fields stay flat so each entry is
> self-contained for LLM consumption; `groups` serves structural consumers.
> The throwaway comparison prototype (`cmd/labelassoc`) that produced §4's
> numbers was deleted after graduation; its behaviours live on as unit tests
> in `pkg/forms/label_test.go`.

All candidates consume the same inputs: `parser.FormFieldBoxes` (widget
rects, top-left origin, plus `/TU`, `/Ff` flags, and `/AP` on-state names) +
`TextCells` (merged line cells) + `WordTextCells` (word-level cells, used for
checkbox option captions).

Shared machinery: candidates in 4 directions with per-direction gap caps
(left 130pt, right 60pt, above 30pt, below 14pt); left/right require ≥50%
vertical overlap, above/below require horizontal overlap with cross-axis
alignment = best of left-edge/centre/right-edge; **occlusion test** (a
candidate is discarded when another widget sits between it and the field);
score = `prior(dir|class) / (1+gap/12) / (1+misalign/24) × labelLikeness`,
where labelLikeness gives ×1.5 for a trailing colon and penalises sentences
(`/(1+0.4×(words−6))` beyond 6 words — LabelEx's length feature).

Widget classes drive the direction priors, from `/Ff` flags alone:
checkbox/radio (radio flag or small square; pushbutton flag excluded) →
right 3, left 2, above 1, below 0.3; comb text field (`/Ff` bit 25) →
below 3, left 2.5, above 1.5, right 0.5; other text fields → left 3,
above 2.5, below 0.8, right 0.5. Signature fields behave like combs
(caption below).

- **A. `naive`** — nearest left, else nearest above. The folklore baseline
  (what accessibility tools and Acrobat's recogniser roughly do).
- **B. `scored`** — the shared scoring, each field labelled in isolation.
- **C. `styled`** — B plus form-style inference: confident single-direction
  wins vote per class, vote share reweights the priors, rescore.
- **D. `clustered`** — B plus **visual field grouping** plus **global
  exclusive assignment**: union same-class Tx/Sig widgets that are adjacent
  (same band, gap ≤ 12pt; or stacked, x-overlap ≥ 80%, gap ≤ 4pt) into
  clusters, score each cluster's enclosing box as one unit, then assign
  captions globally, highest score first, each text cell claimable by only
  one unit (OPAL's exclusivity/interleaving constraint). Members inherit the
  cluster label.
- **Group labels** (orthogonal) — numbered-question anchors (`^\d{1,2}\.`
  line cells); every field down to the next anchor inherits the anchor's
  full line text as question context.

## 4. KS2 evaluation

Facts from the page dump: 38 widgets, 88 line cells, 411 word
cells; **no `/TU` on any field**, untagged; all Tx fields carry
comb+DoNotScroll+DoNotSpellCheck (`/Ff` = 0x1C00000); Yes/No checkboxes have
semantic on-states (`Yes`/`No`) while title/rate checkboxes have generic ones
(`A`–`E`); `/T` names are human-readable ("first names", "town/city").
So on this form rungs 1–2 of the cascade are empty, rung 3 (on-states) is
half-usable, and geometry has to do the real work — exactly the situation the
algorithm must handle.

Accuracy against visually-verified ground truth (37 labelable widgets; the
reset pushbutton draws its caption on its own face and is correctly left
unlabelled by all candidates):

| Algorithm | Correct | Notes |
|---|---|---|
| A naive | 3/37 (8%) | Systematic off-by-one: picks the *previous* field's caption everywhere below-captions or right-captions are used. |
| B scored | 30/37 (81%) | All checkboxes and single-widget combs right; fails on multi-widget clusters (phone 2/6, date 2/3, email 2nd row). |
| C styled | ≈B | Vote reweighting added no wins and (before the length penalty) broke one correct field — vote pollution from cluster failures amplifies errors. Negative result; dropped. |
| D clustered | **37/37 (100%)** | Clustering + exclusivity fixed every remaining miss. |

Two instructive failure-modes discovered en route:

- The email cluster preferred a 13-word footnote sitting 4pt below over the
  true caption 3pt to the left — fixed by the label-likeness length penalty,
  not by geometry.
- After the length penalty, the email cluster then stole "Day" (the phone
  row's caption, 1pt above the email row) by a 1% score margin — fixed by
  global exclusive assignment: "Day" scores far higher as the phone
  cluster's below-caption, so the phone cluster claims it first and email
  falls back to its correct left caption. Local scoring alone cannot resolve
  dense grid forms; some form of matching is load-bearing.

Group labels resolved to the right question for every field ("Are you a
KiwiSaver member? …", "Your postal address", "I declare that…"). Known
noise: option words leak into the question line, and a hard line-wrap
truncates one anchor ("Your contact" without "numbers").

## 5. Recommendation

Cascade, in order:

1. `/TU` when non-empty → done. (Not hypothetical-only: many government
   forms have it; KS2 does not.)
2. Tagged-PDF `Form` struct elem `/Alt` / associated `Lbl` when tagged.
3. Geometric association = algorithm **D**: class priors from `/Ff` flags,
   shared directional scoring with occlusion + label-likeness, same-class
   adjacency clustering, global exclusive caption assignment. Emit:
   - `Label` — the field's (cluster's) own caption;
   - `GroupLabel` — nearest enclosing anchor (numbered question, or a bold
     line/section heading above the field's band);
   - checkbox/radio: cross-check the geometric caption against the `/AP`
     on-state name (`/Opt` when present): agreement → high confidence
     (KS2 Yes/No boxes); generic state names (`A`–`E`, `1`, `Check Box3`)
     → geometry wins.
4. `/T` name tokens as tie-breaker (token overlap with candidate text) and
   last-resort label after splitting separators/camelCase.

Per-field confidence worth emitting: chosen direction, gap, whether the
caption was contested during assignment, and agreement across rungs 1/3/4 —
plus a **provenance tag** per label (Chromium's `LabelSource` taxonomy: tu,
struct-alt, on-state, left, above, right-of-checkbox, cluster, group, name).

Refinements the literature supports, for a broader corpus than one form:

- **Density-normalised distance** — LabelEx's published negative result on
  raw pixel distance; replace the fixed 12pt decay constant with row-pitch
  normalisation when tuning against a corpus.
- **Below-widget overlap = help text** — Chromium's overlay rule: text
  overlapping the widget that extends below its bottom edge is error/help
  text, not a label. Complements the length penalty on the KS2 email
  footnote pattern.
- **Shared-label splitting** — 2–3 consecutive fields tied to one short
  caption ("City/State/Zip"): split on `/ , & -`/"and" when components ==
  fields (Chromium M120; also OPAL's documented failure mode).
- **Blind-spot sectors** — Dragut et al.: never take diagonal-corner text;
  our 4-direction classifier already enforces this implicitly.
- Style voting (C) is subsumed by flag-derived class priors (Dragut R7 is
  its principled form) — revisit only if a corpus shows below-caption
  conventions on non-comb fields; ruling-line detection
  (`Page.RulingSegments`) can recover the comb prior for forms that draw
  boxes instead of setting the comb flag.

Published accuracy ceiling for heuristic stacks of this shape: ~92–96%
(LITE 93%, Dragut 92%, LabelEx F 0.86–0.95, OPAL F 96.3%), with residuals
concentrated in: one text run spanning multiple labels, group labels over
label-less clusters, image-only labels, and equidistant ties.

## 6. References

- Raghavan & Garcia-Molina, *Crawling the Hidden Web*, VLDB 2001 —
  [Stanford TR 2000-36](http://ilpubs.stanford.edu:8090/725/1/2001-19.pdf) ·
  [WWW10 extended abstract](https://archives.iw3c2.org/www10/cdrom/posters/1049.pdf)
  (LITE: four closest candidates + relative-position heuristics, 93%).
- H. Nguyen, T. Nguyen, J. Freire, *Learning to Extract Form Labels*,
  PVLDB 1(1) 2008 —
  [paper](https://www.academia.edu/11423125/Learning_to_extract_form_labels)
  (classifier ensemble + reconciliation).
- Furche, Gottlob, Grasso, Guo, Orsi, Schallhart, *OPAL: Automated Form
  Understanding for the Deep Web*, WWW 2012 —
  [proceedings PDF](https://www2012.universite-lyon.fr/proceedings/proceedings/p829.pdf) ·
  extended: [*The Ontological Key*, VLDB J. 22 (2013)](https://arxiv.org/pdf/1210.5980)
  (field/segment/page scopes, >98% on ICQ/TEL-8).
- Dragut, Kabisch, Yu, Leser, *A Hierarchical Approach to Model Web Query
  Interfaces for Web Source Integration*,
  [PVLDB 2(1) 2009](https://vldb.org/pvldb/vol2/vldb09-360.pdf)
  (design rules R2/R6/R7, sector model, 92% leaf accuracy).
- Uckun, Aydin, Ashok, Ramakrishnan, *Taming User-Interactive PDFs with
  Accessibility Overlays*,
  [PACM EICS 2020](https://pmc.ncbi.nlm.nih.gov/articles/PMC8320357/)
  (PDF-native 4-direction association, F1 0.811; 44.6% meaningful-/TU stat).
- Chromium autofill label inference —
  [form_autofill_util.cc](https://source.chromium.org/chromium/chromium/src/+/main:components/autofill/content/renderer/form_autofill_util.cc)
  (`InferLabelForElement`: checkable → following text first; overlay rule;
  punctuation-only labels rejected; `LabelSource` provenance) ·
  [M120 shared-label splitting](https://chromium.googlesource.com/chromium/src/+/refs/tags/120.0.6099.5/components/autofill/core/browser/form_processing/label_processing_util.cc).
- Adobe, *Designing forms for auto field detection in Adobe Acrobat* —
  [whitepaper PDF](https://acrobatusers.com/assets/collections/tutorials/legacy/id_2263/acro9_designforms.pdf)
  (per-widget-type label direction table; exclusivity authoring rule).
- FormFyxer (Suffolk LIT Lab) —
  [pdf_wrangling.py](https://github.com/SuffolkLITLab/FormFyxer/blob/main/formfyxer/pdf_wrangling.py)
  (50pt dilation, edge-alignment distance, label dedup).
- PDF 32000-1: §12.7.3.1 Table 220 (`/TU`, not inheritable), §12.7.4.2.3–4
  (checkbox/radio, `/Opt` Table 227 inheritable), Table 226 (Btn flags:
  Radio bit 16 = 1<<15, Pushbutton bit 17 = 1<<16), Table 228 (Tx flags:
  Comb bit 25 = 1<<24) —
  [Adobe copy](https://opensource.adobe.com/dc-acrobat-sdk-docs/pdfstandards/PDF32000_2008.pdf).
- PDF/UA-1 (ISO 14289-1) §7.18;
  [Matterhorn Protocol](https://pdfa.org/resource/the-matterhorn-protocol/)
  checkpoints 28-005 (missing `/TU`/`/Alt`) and 28-010 (widget not in
  `Form` tag). Note the PDF Association's
  [Tagged PDF Best Practice Guide](https://pdfa.org/wp-content/uploads/2019/06/TaggedPDFBestPracticeGuideSyntax.pdf)
  §4.3.3.2: ISO 32000 provides **no mechanism** to associate page-content
  labels with fields — even tagged PDFs rely on `Lbl` proximity, so the
  geometric associator stays load-bearing.
