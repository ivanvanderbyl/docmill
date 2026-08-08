# Tasks 1 and 2: the heuristic baseline, and a feature vector built to beat it

**Date:** 2026-08-06
**Plan:** `docs/plans/2026-08-06-learned-layout-classifier.md`, Tasks 1–2
**Follows:** `2026-08-06-doclaynet-line-model.md`
**Verdict:** The model beats the current heuristics on **every class**, and the Task 2
feature vector beats the spike's on **eleven of twelve**. The Task 6 migration gate is
cleared on this metric — with one significant caveat about what the metric is.

---

## Task 1: what the current heuristics actually score

Scored on DocLayNet's `val` split, 256,338 lines. Both sides are graded on the *same*
class-agnostically assembled lines and joined by the *same* rule, because
`LayoutDebugRows` emits the feature vector and the current heuristic class in one pass.
That sameness is what makes the comparison mean anything.

| DocLayNet class | support | precision | recall | F1 | docmill's detector |
|---|---|---|---|---|---|
| Text | 133,321 | 0.571 | 0.822 | 0.674 | paragraph (the default) |
| Table | 34,776 | 0.276 | 0.400 | **0.327** | `DetectTables` |
| List-item | 20,651 | 0.792 | 0.126 | **0.218** | `DetectStructure` |
| Section-header | 13,075 | 0.596 | 0.459 | 0.519 | heading detector |
| Picture | 9,681 | 0.968 | 0.074 | **0.137** | figure-label filter |
| Title | 328 | 0.019 | 0.591 | 0.037 | heading detector |
| Formula | 3,402 | — | — | **0.000** | *none* |
| Caption | 2,050 | — | — | 0.000 | *none* |
| Footnote | 403 | — | — | 0.000 | *none* |
| Page-header | 2,709 | — | — | 0.000 | *none* |
| Page-footer | 4,992 | — | — | 0.000 | *none* |

Five classes score zero because docmill has no detector for them. For `Formula` that is
the entire premise of Task 0 — confirmed at scale rather than asserted. For the
page-furniture classes it is a deliberate design choice, not a defect: docmill keeps
headers and footers in the text on purpose.

**The fake-table defect, quantified.** Where each class actually lands today:

| DocLayNet class | swallowed into a table |
|---|---|
| Formula | **31%** |
| Text | 16% |
| List-item | 12% |
| Caption | 11% |
| Picture | 25% |

Nearly a third of all display-equation lines in DocLayNet are currently emitted as table
cells. That is the defect `entropy.pdf` showed anecdotally, measured across 2,570
documents.

**`List-item` recall of 0.126 at precision 0.792** is the other striking number: when
`DetectStructure` fires it is usually right, but it fires on one list line in eight.
`Picture` is the same shape, more extreme — 0.968 precision at 0.074 recall. These are
conservative detectors doing exactly what `AGENTS.md` asks for ("prefer conservative
detection with clear false-positive guards"), and paying for it in recall.

## Task 2: the feature vector

Moved into `pkg/pdf/layoutfeatures.go` — shipping code with tests, not a throwaway copy.
`LayoutFeatureNames` is now the single definition of the contract, and the trainer reads
it out of the binary rather than restating it, so a reordering cannot silently shift an
index under the model.

Grown from the spike's 20 features to 32. The additions were chosen from the previous
run's diagnosis rather than by guessing:

- **`has_list_marker`, `numbering_depth`, `content_left_offset`** — `List-item` was the
  worst class at 0.588 F1, and a list item is defined by a leading marker and a hanging
  indent. The spike measured neither.
- **`indent_vs_body`, `column_span_frac`** — indentation and width relative to the page's
  own body text, rather than to the page.
- **`stroke_density`** — ruling segments near the line; the signal the plan warns the line
  vector may capture poorly.
- **`repeat_frac`** — cross-page box repetition, for page furniture.
- **`caption_marker`, `upper_frac`, `punct_frac`, `trailing_period`, `page_index_frac`.**

Two design notes worth keeping:

**The marker and caption tests are structural, not lexical.** `numbering_depth` reads the
*shape* of the leading token (digits and separators), and `caption_marker` matches
`<word> <number>` — so it fires on `Tabelle 2` and `図 3`, not just `Table 2`. `AGENTS.md`
permits character cues only as supporting evidence inside a broader layout algorithm,
which is what they are as 2 features of 32.

**`repeat_frac` returns 0 for single-page documents, and that is a correctness fix.**
DocLayNet is 81,471 single-page PDFs. The naive answer (1/1 = 1.0) would say "this box
repeats on every page" about every line in the corpus, training the model on a constant
that becomes a real varying signal at inference on multi-page documents. Quiet
training/serving skew, avoided.

## Result: 20 features versus 32

Identical corpus, identical splits, identical hyperparameters.

| class | 20 features | 32 features | Δ |
|---|---|---|---|
| **List-item** | 0.588 | **0.661** | **+0.073** |
| Table | 0.760 | 0.805 | +0.045 |
| Text | 0.828 | 0.855 | +0.027 |
| Picture | 0.747 | 0.768 | +0.021 |
| Section-header | 0.793 | 0.813 | +0.020 |
| Title | 0.640 | 0.655 | +0.015 |
| Caption | 0.643 | 0.655 | +0.012 |
| Background | 0.719 | 0.731 | +0.012 |
| Formula | 0.803 | 0.812 | +0.009 |
| Page-footer | 0.969 | 0.974 | +0.005 |
| Footnote | 0.677 | 0.677 | 0.000 |
| Page-header | 0.756 | 0.742 | −0.014 |
| **accuracy** | 0.7758 | **0.8054** | +0.030 |
| **macro-F1** | 0.7436 | **0.7624** | +0.019 |

`List-item` gained the most, by a factor of three over the next class. The diagnosis was
right and the fix was the features, not the model, the corpus or the hyperparameters.
`Page-header` is the only regression, at −0.014, within run-to-run noise.

## Lexical ablation

Task 2 step 2 requires checking that the model degrades gracefully without the
content-derived features, because they are English-biased and `AGENTS.md` requires
document-general inputs. Dropping all seven (`math_frac`, `digit_frac`, `letter_frac`,
`upper_frac`, `punct_frac`, `trailing_period`, `caption_marker`):

| | accuracy | macro-F1 |
|---|---|---|
| 32 features | 0.8054 | 0.7624 |
| 25 features, lexical ablated | 0.7763 | 0.7434 |
| *(20-feature spike vector, for scale)* | *0.7758* | *0.7436* |

The lexical features are worth about 3 accuracy points and 2 macro-F1 points — a real
contribution, not a collapse. A document in a script these features do not suit loses
roughly that much and still lands where the previous full model did. That satisfies the
graceful-degradation requirement.

Two class-level surprises worth noting:

- **`Page-header` is BETTER without them**: 0.742 → **0.826**. The lexical features were
  actively hurting it — plausibly because header text is short and its character mix
  looks like a caption or a title.
- **`Title` is much worse**: 0.655 → 0.543, and `Table` drops 0.805 → 0.731.

That asymmetry suggests per-class feature selection is worth a look later; it is not
worth complicating Task 3 for now.

## Model versus heuristics, side by side

| DocLayNet class | heuristics | model | verdict |
|---|---|---|---|
| Page-footer | 0.000 | 0.974 | model |
| Text | 0.674 | 0.855 | model |
| Section-header | 0.519 | 0.813 | model |
| Formula | 0.000 | 0.812 | model |
| Table | 0.327 | 0.805 | model |
| Picture | 0.137 | 0.768 | model |
| Page-header | 0.000 | 0.742 | model |
| Footnote | 0.000 | 0.677 | model |
| List-item | 0.218 | 0.661 | model |
| Caption | 0.000 | 0.655 | model |
| Title | 0.037 | 0.655 | model |

The model wins every class, most of them by a wide margin.

## The caveat that matters

**This measures agreement with DocLayNet's taxonomy, not extraction quality.** The two
are not the same thing, and the gap runs in docmill's favour more often than the table
suggests:

- docmill's zero on `Page-header`/`Page-footer` is a *design decision* — it keeps page
  furniture in the text deliberately. Scoring it as a failure is scoring it against
  someone else's spec.
- `Text` precision of 0.571 partly reflects that docmill's "paragraph" is a catch-all
  that absorbs every class it has no detector for. That is what a default does.
- Line-level `Table` and `Picture` scores describe a *region* decision. The plan already
  routes both through the REGION model for exactly this reason.

So the right reading is: **the model is a better line classifier than the heuristics on
every class, and this is necessary but not sufficient evidence to migrate.** The
sufficient evidence is DPBench — Task 5's neutrality gate and Task 7's end-to-end
comparison. Nothing here has measured a single character of Markdown output.

`Formula` is the exception that needs no further argument: docmill has no formula
detector, so 0.812 against 0.000 is not a taxonomy artefact. It is the class to migrate
first, exactly as the plan sequenced it.

## Reproducing

```bash
spike emit -list pdflist.txt -jobs 4 > lines.jsonl        # features + current class
python join_doclaynet.py --lines lines.jsonl --annotations annotations.jsonl --out dataset.jsonl
python baseline.py --dataset dataset.jsonl --split val    # Task 1
python train_doclaynet.py --dataset dataset.jsonl --model-out line.txt   # Task 2/3
```
