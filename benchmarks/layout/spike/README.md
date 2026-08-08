# Layout classifier harness

Training and measurement for the learned layout classifier
(`docs/plans/2026-08-06-learned-layout-classifier.md`). **Not shipping code** —
the feature vector, the model and the routing all live in `pkg/pdf`. This drives
them over a corpus.

## Contents

| path | what |
|---|---|
| `cmd/spike` | `emit` (features + current class per line), `explain`, `features` |
| `cmd/packmodel` | LightGBM text model → the blob `pkg/pdf` embeds |
| `cmd/reroutecheck` | Task 5 neutrality gate over DPBench, plus straddle risk |
| `cmd/formulacheck` | proves the Formula veto runs, and on what |
| `fetch_annotations.py` | DocLayNet labels via column-selective parquet reads |
| `join_doclaynet.py` | DocLayNet regions → per-line labels |
| `train_doclaynet.py` | the 12-class LINE model |
| `baseline.py` | Task 1: score the current heuristics on the same lines |
| `goenv.sh` | Go toolchain and cache paths |

## Pipeline

```bash
source goenv.sh
python fetch_annotations.py annotations.jsonl        # labels
spike emit -list pdflist.txt -jobs 4 > lines.jsonl   # docmill's own features
python join_doclaynet.py --lines lines.jsonl --annotations annotations.jsonl --out dataset.jsonl
python baseline.py --dataset dataset.jsonl --split val
python train_doclaynet.py --dataset dataset.jsonl --model-out line.txt
packmodel -in line.txt -out ../../../pkg/pdf/layoutmodel.bin -classes line.txt.classes.json
go test ./pkg/pdf/ -run TestLayoutModel     # Go and Python must agree exactly
```

**Retraining is not optional after a line-assembly change.** The model
classifies assembled lines, so a change to the assembler changes its input. This
has bitten once already: a merge moved `entropy.pdf` from 2,551 lines to 2,335
and the model was scoring lines that no longer existed.

## Removed

The Task 0 spike — HURIDOCS teacher labels, the binary Formula model, the
`leaves` runtime and the Go-source codegen — is deleted. Its conclusions are
recorded in `docs/research/2026-08-06-formula-classifier-spike.md`, and it is
recoverable from git history. What replaced it: human DocLayNet labels instead
of a model teacher, a 12-class model instead of binary, and a packed blob in
`pkg/pdf` instead of generated Go.

Working data (corpora, datasets, model artefacts) lives outside the repo.
