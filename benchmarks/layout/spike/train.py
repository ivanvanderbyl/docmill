#!/usr/bin/env python3
"""Train the Task 0 binary Formula classifier and emit the Go-embeddable model.

Task 0 step 3 of docs/plans/2026-08-06-learned-layout-classifier.md.

Two things are held out, for two different reasons:

  * entropy.pdf never enters training or model selection at all. Step 5 grades
    the spike on it against the current heuristics, and a spike that grades
    itself on its own training document proves nothing.
  * Within the remaining corpus the split is by DOCUMENT, not by line. Lines
    from one paper share a template, a font stack and an equation style; a
    random line split would let the model memorise the document and report a
    score it cannot repeat on an unseen one.

Reproducibility is pinned per the plan: fixed seed, ``deterministic=true``,
single thread, and ``force_row_wise`` to stop LightGBM choosing a histogram
construction strategy from machine timing. Two runs over the same data produce
byte-identical models.
"""

import argparse
import collections
import json
import subprocess
import sys

import lightgbm as lgb
import numpy as np
from sklearn.model_selection import GroupKFold

# Pinned training configuration -- see the reproducibility note above.
SEED = 20260806
PARAMS = {
    "objective": "binary",
    "learning_rate": 0.05,
    "num_leaves": 31,
    "max_depth": 6,
    "min_data_in_leaf": 20,
    "feature_fraction": 0.9,
    "bagging_fraction": 0.9,
    "bagging_freq": 1,
    "seed": SEED,
    "deterministic": True,
    "force_row_wise": True,
    "num_threads": 1,
    "verbosity": -1,
}
NUM_ROUNDS = 300
HELD_OUT_DOC = "entropy"


def load(path):
    rows = [json.loads(line) for line in open(path)]
    features = np.array([r["f"] for r in rows], dtype=np.float64)
    labels = np.array([r["label"] for r in rows], dtype=np.int32)
    docs = np.array([r["doc"] for r in rows])
    return rows, features, labels, docs


def feature_contract(binary):
    """Read the feature-name list straight out of the Go emitter.

    The names are a contract between the two languages. Asking the binary for
    them -- rather than restating them here -- means a reordering in
    features.go cannot silently shift an index under the trainer.
    """
    out = subprocess.run([binary, "features"], capture_output=True, check=True, text=True)
    return json.loads(out.stdout)


def relabel_model_version(path):
    """Rewrite the model header's ``version=v4`` to ``version=v3``.

    github.com/dmitryikh/leaves accepts only v2 and v3 in its text-model header
    (lgensemble_io.go: ``params.Compare("version", "v3")``); LightGBM has
    stamped v4 since 4.0. The guard is a version check, not a parser branch --
    everything leaves reads afterwards (num_class, num_tree_per_iteration,
    max_feature_idx, tree_sizes, then the Tree= blocks) is byte-identical
    between the two.

    That is an assertion, so it is checked rather than believed: ``spike
    verify`` replays LightGBM's own scores for twenty vectors through leaves and
    fails on any disagreement beyond 1e-9. Do not trust this rewrite without
    that step passing.

    Recorded as a Task 4 decision: either keep this one-line rewrite, pin
    LightGBM 3.x (EOL, no wheels past CPython 3.11), or switch to codegen --
    which the plan already keeps in reserve and which sidesteps leaves entirely.
    """
    with open(path) as handle:
        text = handle.read()
    if not text.startswith("tree\nversion=v4\n"):
        return
    with open(path, "w") as handle:
        handle.write(text.replace("tree\nversion=v4\n", "tree\nversion=v3\n", 1))


def scores(labels, predicted):
    tp = int(np.sum((predicted == 1) & (labels == 1)))
    fp = int(np.sum((predicted == 1) & (labels == 0)))
    fn = int(np.sum((predicted == 0) & (labels == 1)))
    precision = tp / (tp + fp) if tp + fp else 0.0
    recall = tp / (tp + fn) if tp + fn else 0.0
    f1 = 2 * precision * recall / (precision + recall) if precision + recall else 0.0
    return tp, fp, fn, precision, recall, f1


def train(features, labels, names, pos_weight):
    weights = np.where(labels == 1, pos_weight, 1.0)
    dataset = lgb.Dataset(features, label=labels, weight=weights, feature_name=names)
    return lgb.train(PARAMS, dataset, num_boost_round=NUM_ROUNDS)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--dataset", default="out/dataset.jsonl")
    parser.add_argument("--model-out", default="cmd/spike/layoutmodel.txt")
    parser.add_argument("--fixture-out", default="out/fixture.json")
    parser.add_argument("--binary", default="../../../bin/spike")
    parser.add_argument("--pos-weight", type=float, default=3.0)
    parser.add_argument("--folds", type=int, default=5)
    args = parser.parse_args()

    rows, features, labels, docs = load(args.dataset)
    names = feature_contract(args.binary)
    if features.shape[1] != len(names):
        sys.exit(
            f"feature contract mismatch: dataset has {features.shape[1]} columns, "
            f"emitter declares {len(names)} names"
        )

    train_mask = docs != HELD_OUT_DOC
    print(
        f"corpus: {len(rows)} lines / {len(set(docs))} documents; "
        f"training pool {int(train_mask.sum())} lines "
        f"({int(labels[train_mask].sum())} Formula), "
        f"{HELD_OUT_DOC} held out entirely "
        f"({int((~train_mask).sum())} lines, {int(labels[~train_mask].sum())} Formula)"
    )

    pool_x, pool_y, pool_g = features[train_mask], labels[train_mask], docs[train_mask]

    # Grouped cross-validation over whole documents: the held-out report.
    folds = min(args.folds, len(set(pool_g)))
    oof = np.zeros(len(pool_y))
    for fold, (tr, va) in enumerate(GroupKFold(n_splits=folds).split(pool_x, pool_y, pool_g)):
        booster = train(pool_x[tr], pool_y[tr], names, args.pos_weight)
        oof[va] = booster.predict(pool_x[va])
        tp, fp, fn, p, r, f1 = scores(pool_y[va], (oof[va] >= 0.5).astype(int))
        print(
            f"  fold {fold}: docs={sorted(set(pool_g[va]))[:3]}... "
            f"P={p:.3f} R={r:.3f} F1={f1:.3f} (tp={tp} fp={fp} fn={fn})"
        )

    tp, fp, fn, p, r, f1 = scores(pool_y, (oof >= 0.5).astype(int))
    print(f"\nheld-out documents (pooled out-of-fold): P={p:.3f} R={r:.3f} F1={f1:.3f}")
    print(f"  tp={tp} fp={fp} fn={fn}")

    # Final model: every training document, nothing from entropy.pdf.
    booster = train(pool_x, pool_y, names, args.pos_weight)
    booster.save_model(args.model_out)
    relabel_model_version(args.model_out)
    print(f"\nwrote {args.model_out} ({booster.num_trees()} trees)")

    importance = sorted(
        zip(names, booster.feature_importance("gain")), key=lambda kv: -kv[1]
    )
    print("\ntop features by gain:")
    for name, gain in importance[:10]:
        print(f"  {name:22s} {gain:12.1f}")

    # Fixture pinning the Python/Go agreement (plan Task 4 step 3). A stratified
    # handful of vectors plus the scores LightGBM produced for them; the Go side
    # asserts against these, which is what catches an index shift or float drift.
    order = np.argsort(-booster.predict(pool_x))
    picks = list(order[:10]) + list(order[len(order) // 2 : len(order) // 2 + 5]) + list(order[-5:])
    fixture = {
        "features": names,
        "cases": [
            {"f": pool_x[i].tolist(), "score": float(booster.predict(pool_x[i : i + 1])[0])}
            for i in picks
        ],
    }
    with open(args.fixture_out, "w") as handle:
        json.dump(fixture, handle, indent=1)
    print(f"wrote {args.fixture_out} ({len(fixture['cases'])} cases)")

    counts = collections.Counter(docs[~train_mask])
    print(f"\n{HELD_OUT_DOC} remains unseen: {dict(counts)}")


if __name__ == "__main__":
    main()
