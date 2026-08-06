#!/usr/bin/env python3
"""Train the 11-class LINE model on DocLayNet, with docmill's own features.

Differences from train.py, which trains the spike's binary Formula model:

  * 12 classes — DocLayNet's 11, plus `Background` for assembled lines no
    annotated region covers (page furniture, extraction artefacts, and text the
    annotators left outside every box). Background is a real prediction target,
    not a discard: at inference docmill sees these lines and must do something
    with them.
  * DocLayNet's OWN train/val/test splits, not a home-made GroupKFold. They are
    built to keep a document's pages together and to balance the class mix, so
    using them makes the number comparable to published work on this dataset.
    `test` is never touched here.
  * Class weights rather than a single positive weight. Text dominates by an
    order of magnitude, and docmill's costs are asymmetric — a fake table is
    worse than a missed one. The asymmetry belongs in training weights; the plan
    is explicit that inference stays pure argmax with no per-class thresholds.

Reproducibility is pinned exactly as in train.py: fixed seed,
`deterministic=true`, single thread, `force_row_wise`.
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
    "objective": "multiclass",
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
    # Reproducibility needs the thread count PINNED, not set to 1: LightGBM's
    # histogram reduction is deterministic for a given number of threads, so any
    # fixed value reproduces bit-for-bit. train.py uses 1 because the spike
    # corpus is small; at 2.6M rows that would cost hours for no benefit.
    # Changing this value changes the model, so it is recorded with it.
    "num_threads": 4,
    "verbosity": -1,
}


def feature_contract(binary):
    out = subprocess.run([binary, "features"], capture_output=True, check=True, text=True)
    return json.loads(out.stdout)


def load(path, names):
    splits = collections.defaultdict(lambda: {"x": [], "y": [], "doc": []})
    classes = set()
    with open(path) as handle:
        for raw in handle:
            row = json.loads(raw)
            bucket = splits[row["split"]]
            bucket["x"].append(row["f"])
            bucket["y"].append(row["cls"])
            bucket["doc"].append(row["source_doc"])
            classes.add(row["cls"])
    ordered = sorted(classes)
    index = {name: i for i, name in enumerate(ordered)}
    out = {}
    for split, bucket in splits.items():
        features = np.array(bucket["x"], dtype=np.float64)
        if features.shape[1] != len(names):
            sys.exit(f"feature contract mismatch: data has {features.shape[1]} columns, emitter declares {len(names)}")
        out[split] = (
            features,
            np.array([index[v] for v in bucket["y"]], dtype=np.int32),
            np.array(bucket["doc"]),
        )
    return out, ordered


def report(title, y_true, y_pred, classes):
    print(f"\n=== {title} ===")
    print(f"{'class':16s} {'support':>8s} {'precision':>10s} {'recall':>8s} {'F1':>8s}")
    macro = []
    for i, name in enumerate(classes):
        tp = int(np.sum((y_pred == i) & (y_true == i)))
        fp = int(np.sum((y_pred == i) & (y_true != i)))
        fn = int(np.sum((y_pred != i) & (y_true == i)))
        precision = tp / (tp + fp) if tp + fp else 0.0
        recall = tp / (tp + fn) if tp + fn else 0.0
        f1 = 2 * precision * recall / (precision + recall) if precision + recall else 0.0
        macro.append(f1)
        print(f"{name:16s} {tp + fn:8d} {precision:10.3f} {recall:8.3f} {f1:8.3f}")
    accuracy = float(np.mean(y_pred == y_true))
    print(f"{'-' * 52}\naccuracy {accuracy:.4f}   macro-F1 {float(np.mean(macro)):.4f}")
    return accuracy, float(np.mean(macro))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--dataset", required=True)
    parser.add_argument("--model-out", default="out/doclaynet_line.txt")
    parser.add_argument("--rounds", type=int, default=300)
    parser.add_argument("--binary", default="../../../bin/spike")
    parser.add_argument("--no-class-weights", action="store_true")
    args = parser.parse_args()

    names = feature_contract(args.binary)
    splits, classes = load(args.dataset, names)
    for split in ("train", "val"):
        if split not in splits:
            sys.exit(f"dataset has no {split} split")

    train_x, train_y, train_doc = splits["train"]
    val_x, val_y, _ = splits["val"]
    print(f"classes ({len(classes)}): {classes}")
    print(f"train {len(train_y)} lines / {len(set(train_doc))} source documents")
    print(f"val   {len(val_y)} lines")
    if "test" in splits:
        print(f"test  {len(splits['test'][1])} lines (untouched)")

    # Inverse-frequency weights, square-rooted so the rare classes are lifted
    # without letting a 26-line class dominate the loss.
    if args.no_class_weights:
        weights = np.ones(len(train_y))
    else:
        counts = np.bincount(train_y, minlength=len(classes)).astype(np.float64)
        per_class = np.sqrt(counts.max() / np.maximum(counts, 1))
        weights = per_class[train_y]
        print("\nclass weights: " + ", ".join(f"{c}={per_class[i]:.2f}" for i, c in enumerate(classes)))

    params = dict(PARAMS)
    params["num_class"] = len(classes)
    booster = lgb.train(
        params,
        lgb.Dataset(train_x, label=train_y, weight=weights, feature_name=names),
        num_boost_round=args.rounds,
    )

    report("validation (DocLayNet val split)", val_y, np.argmax(booster.predict(val_x), axis=1), classes)

    booster.save_model(args.model_out)
    with open(args.model_out + ".classes.json", "w") as handle:
        json.dump(classes, handle)
    dump = booster.dump_model()
    nodes = sum(t["num_leaves"] - 1 for t in dump["tree_info"])
    print(f"\nwrote {args.model_out}: {booster.num_trees()} trees, {nodes} nodes")

    print("\ntop features by gain:")
    for name, gain in sorted(zip(names, booster.feature_importance("gain")), key=lambda kv: -kv[1])[:12]:
        print(f"  {name:22s} {gain:14.1f}")


if __name__ == "__main__":
    main()
