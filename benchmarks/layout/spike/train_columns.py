#!/usr/bin/env python3
"""Train the column-boundary model on FinTabNet.

The layout classifier answers "is this region a table". This answers "where are
its columns", which is the part a class label cannot express and which TEDS
actually scores.

The split is by TABLE, using FinTabNet's own train/val files. Grouping matters
less here than it did for lines — a table's candidate gaps are not shared with
any other table — but tables from one company's report share a house style, so
the published split is still the honest one to use.

Reproducibility is pinned exactly as the line model: fixed seed,
deterministic=true, fixed thread count, force_row_wise.
"""

import argparse
import collections
import json
import subprocess
import sys

import lightgbm as lgb
import numpy as np

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
    "num_threads": 4,
    "verbosity": -1,
}


def load(path):
    features, labels, tables = [], [], []
    with open(path) as handle:
        for raw in handle:
            row = json.loads(raw)
            features.append(row["f"])
            labels.append(row["label"])
            tables.append(row["table_id"])
    return np.array(features, dtype=np.float64), np.array(labels, dtype=np.int32), np.array(tables)


def report(title, y, p):
    predicted = (p >= 0.5).astype(int)
    tp = int(np.sum((predicted == 1) & (y == 1)))
    fp = int(np.sum((predicted == 1) & (y == 0)))
    fn = int(np.sum((predicted == 0) & (y == 1)))
    precision = tp / (tp + fp) if tp + fp else 0.0
    recall = tp / (tp + fn) if tp + fn else 0.0
    f1 = 2 * precision * recall / (precision + recall) if precision + recall else 0.0
    print(f"\n=== {title} ===")
    print(f"  candidates {len(y)}, real boundaries {int(y.sum())}")
    print(f"  precision {precision:.4f}  recall {recall:.4f}  F1 {f1:.4f}")
    print(f"  tp={tp} fp={fp} fn={fn}")
    return precision, recall, f1


def per_table_exact(y, p, tables):
    """Fraction of tables whose column set is recovered EXACTLY.

    The per-candidate score flatters: a table with nine boundaries where eight
    are right still has the wrong grid, and TEDS will score it as wrong. This is
    the number that corresponds to what a reader sees.
    """
    predicted = (p >= 0.5).astype(int)
    grouped = collections.defaultdict(lambda: [0, 0])
    for table, truth, guess in zip(tables, y, predicted):
        grouped[table][0] += 1
        if truth == guess:
            grouped[table][1] += 1
    exact = sum(1 for total, right in grouped.values() if total == right)
    return exact, len(grouped)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--train", required=True)
    parser.add_argument("--val", required=True)
    parser.add_argument("--model-out", default="out/columns.txt")
    parser.add_argument("--rounds", type=int, default=300)
    parser.add_argument("--binary", default="../../../bin/tablegaps")
    args = parser.parse_args()

    names = json.loads(subprocess.run([args.binary, "-features"], capture_output=True, check=True, text=True).stdout)
    train_x, train_y, _ = load(args.train)
    val_x, val_y, val_tables = load(args.val)
    if train_x.shape[1] != len(names):
        sys.exit(f"feature contract mismatch: data has {train_x.shape[1]} columns, emitter declares {len(names)}")

    print(f"train {len(train_y)} candidates ({train_y.mean():.1%} real)")
    print(f"val   {len(val_y)} candidates ({val_y.mean():.1%} real)")

    booster = lgb.train(
        PARAMS,
        lgb.Dataset(train_x, label=train_y, feature_name=names),
        num_boost_round=args.rounds,
    )

    predictions = booster.predict(val_x)
    report("held-out tables (FinTabNet val)", val_y, predictions)
    exact, total = per_table_exact(val_y, predictions, val_tables)
    print(f"\n  tables with EVERY boundary correct: {exact}/{total} ({exact / max(total, 1):.1%})")

    booster.save_model(args.model_out)
    print(f"\nwrote {args.model_out}: {booster.num_trees()} trees")
    print("\ntop features by gain:")
    for name, gain in sorted(zip(names, booster.feature_importance("gain")), key=lambda kv: -kv[1])[:10]:
        print(f"  {name:22s} {gain:14.1f}")


if __name__ == "__main__":
    main()
