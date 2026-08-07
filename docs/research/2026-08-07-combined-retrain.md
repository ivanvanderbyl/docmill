# The combined retrain: 0.5525 → 0.6275, and the model gets a Markdown path

**Date:** 2026-08-07
**Follows:** `2026-08-07-region-decision-rule.md`
**Verdict:** The three queued fixes — lambda_l2, page-scope gutter features, and a
table-weighted IoU head — retrained together, lift the region stage from **0.5525 to
0.6275** weighted F1 on the seeded random val sample. Feature extraction drops from 291
to 16 ms/page. Two new user-facing surfaces expose the stage on real documents:
`docmill render -regions` and `docmill convert -region-markdown`.

---

## The retrain

One retrain carried all three changes, deliberately: each alone invalidates the others'
artefacts, and the count-based feature-contract check cannot catch a semantic shift in
the gutter slots, so blobs and extractor had to move in one commit.

Per-class, end to end, same random 800 pages, same decision rule:

| class | before | **after** | prec after |
|---|---|---|---|
| Page-footer | 0.732 | **0.825** | 0.869 |
| Section-header | 0.660 | **0.749** | 0.807 |
| Caption | 0.444 | **0.633** | 0.852 |
| List-item | 0.523 | **0.613** | 0.752 |
| Table | 0.536 | **0.609** | 0.685 |
| Footnote | 0.146 | **0.604** | 0.842 |
| Text | 0.554 | **0.600** | 0.795 |
| Formula | 0.444 | **0.586** | 0.764 |
| Picture | 0.449 | **0.585** | 0.614 |
| Title | 0.124 | **0.494** | 0.714 |
| Page-header | 0.345 | **0.434** | 0.616 |
| **weighted** | 0.5525 | **0.6275** | |

Precision is 0.6–0.87 everywhere. Recall is the remaining gap, and its largest single
component is still the proposer (Text reachable 69.6%, Page-header 44.7%).

The regularisation did what it was for: raw scores are back in sane ranges instead of
±1.6 million, and per-candidate F1 rose for every class — Caption 0.35→0.65,
Title 0.13→0.64, Footnote 0.25→0.72. The IoU head held at 0.116 mean error with the
3x table weighting.

## The character-drop investigation

Testing `-region-markdown` produced "the markdown output dropped a lot of characters
and words". Measured on 25 DPBench documents, three distinct causes, in size order:

1. **Table markup, not content.** Char retention read 90.7% while WORD retention read
   98.6% — the worst "regression" document had exactly zero missing words. A table that
   renders as prose loses its pipes, dashes and padding, which is a structure failure
   masquerading as a content failure. Char counts cannot distinguish them; word counts
   can.
2. **Two real bugs in the table branch, fixed.** Only the FIRST table found inside a
   region was rendered, and cells inside the region but outside the detected grid were
   dropped. Both now re-attach.
3. **Misclassified pictures, guarded.** Dropping text inside a Picture region is correct
   for real figures and deletes a paragraph when the classification is wrong. A Picture
   region containing NO ink — no image, no path, no shading — is a misclassification
   wearing a green box, and its text now survives as prose.

Remaining word loss (~1.3%) is chart legends and axis labels inside genuine figures,
dropped by design, matching the annotation standard.

## The user-facing surfaces

```
docmill render -regions -out /tmp/boxes doc.pdf   # one labelled box per region, PNG
docmill render -regions -json doc.pdf             # same decomposition as JSONL
docmill convert -region-markdown doc.pdf          # the model OWNS the page
```

`-region-markdown` is the plan's end state in miniature: the region stage decomposes the
page, tables run the existing grid machinery only inside model-approved boxes, picture
innards are dropped (with the ink guard), headings and lists come from region classes,
and unclaimed lines fall through as paragraphs so nothing silently vanishes. Missing
models degrade to the routed path, never to an empty page.

It is experimental and says so: 0.63 against DocLayNet regions is not yet better than
the tuned pipeline on clean documents. The flag exists so the difference can be seen on
real documents rather than argued from benchmark deltas.

## Status

Default path byte-identical over all 200 DPBench documents against the pre-region
baseline binary. `go vet ./...` and the full suite clean. Feature extraction 16 ms/page
(was 291).

## Next

1. Proposer recall is now the binding constraint everywhere: Text 69.6% and Page-header
   44.7% reachable cap those classes regardless of model quality.
2. Table structure inside accepted regions: 0.685 precision on WHERE; TEDS on the
   region-routed path would say how much of the structure survives.
3. The rand800 protocol note: never sample the head of a sorted file.
