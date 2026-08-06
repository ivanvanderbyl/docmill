# Task 0 spike — throwaway

Answers one question: can a gradient-boosted tree over docmill's own line geometry
separate display equations from headings? Result and numbers:
`docs/research/2026-08-06-formula-classifier-spike.md`.

This directory is **not** shipping code. If the plan proceeds, Task 2 rebuilds the
feature extractor properly inside `pkg/pdf` and this is deleted.

## Layout

| path | what |
|---|---|
| `cmd/spike/` | Go: emitter, `leaves` predictor, fixture check. `featureNames` in `features.go` is the feature contract. |
| `cmd/spike/layoutmodel.txt` | trained artefact, `go:embed`ed |
| `label_all.sh` | runs the HURIDOCS labeller over `pdfs/` |
| `labels/` | teacher output, committed so training reproduces without the 17 GB image |
| `join.py` | teacher `Formula` boxes → line labels, by containment fraction |
| `train.py` | binary LightGBM, split by document, `entropy.pdf` excluded |
| `eval.py` | model vs current heuristics on `entropy.pdf` |
| `goenv.sh` | Go toolchain and cache paths |
| `pdfs/`, `out/` | gitignored |

## Corpus

19 arXiv papers plus `entropy.pdf` (Shannon 1948). `entropy.pdf` is the exam and never
enters training or model selection — `train.py` drops it by name before splitting, and
`eval.py` grades on it.

The arXiv IDs are the filenames: 1207.7214, 1312.6114, 1409.0473, 1412.6980, 1502.03167,
1503.02531, 1508.06576, 1512.03385, 1602.03837, 1607.06450, 1611.03530, 1706.03762,
1710.10903, 1806.07366, 2006.11239, 2010.11929, hep-th/9711200, math/0211159,
quant-ph/9708016. Task 1 replaces this with a manifest recording each document's source
and licence.

## Commands

See the "Reproducing" section of the research note. `go vet ./benchmarks/layout/spike/...`
is the build check — this repo does not use `go build` for verification.
