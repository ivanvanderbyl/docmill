#!/usr/bin/env python3
"""Train the region CLASSIFIER over class-agnostic proposals.

Multiclass, not binary. The previous region model was a gate — it answered
"keep this candidate?" about candidates that already carried a class. This one
assigns the class too, because the new proposer splits on geometry and knows
nothing about what it has found.

That difference is the point rather than a detail. A gate can only reject, so it
can veto a wrong extent but never replace it with the right one. A classifier
produces a comparable confidence for every candidate, and comparable confidence
is what lets overlapping proposals compete in non-max suppression. With ~375
candidates per page the competition is the whole mechanism.

Memory shapes the whole script. The dataset is eight million rows on a machine
with about four gigabytes free, so nothing is ever held twice: the training set
is streamed to a temporary file and read back into one preallocated float32
array, and validation is scored in chunks rather than materialised. An earlier
version kept a Python set of kept row indices and was killed outright by that
alone.
"""

import argparse
import collections
import json
import os
import subprocess
import sys
import tempfile

import lightgbm as lgb
import numpy as np

SEED = 20260806

# Fixed class order. It is written into the model sidecar and read back by the
# Go loader, so a class added here without regenerating the model would silently
# renumber every prediction.
CLASSES = [
    "Background", "Caption", "Footnote", "Formula", "List-item", "Page-footer",
    "Page-header", "Picture", "Section-header", "Table", "Text", "Title",
]
CLASS_INDEX = {name: i for i, name in enumerate(CLASSES)}

# lambda_l2=10 is a correction, not a tuning flourish. With LightGBM's default
# of zero, the unweighted rare classes saturate: leaf values explode until raw
# class scores reach +/-1.6 MILLION, softmax outputs collapse to exact 0s and
# 1s, and the ranking granularity non-max suppression depends on is gone — a
# thousand candidates all "certain" cannot be ordered. The explosion is also
# what finally surfaced the softmax aliasing bug as Inf/NaN.
PARAMS = {
    "objective": "multiclass",
    "num_class": len(CLASSES),
    "learning_rate": 0.08,
    "num_leaves": 63,
    "max_depth": 8,
    "min_data_in_leaf": 50,
    "lambda_l2": 10.0,
    "feature_fraction": 0.9,
    "bagging_fraction": 0.8,
    "bagging_freq": 1,
    "seed": SEED,
    "deterministic": True,
    "force_row_wise": True,
    "num_threads": 8,
    "verbosity": -1,
}

# Background is 87.6% of the dataset and it does not fit. Something has to go,
# and WHICH negatives go matters far more than how many.
#
# A Background candidate overlapping a real region is a near miss — a table one
# line short, a paragraph merged with the heading above it. Those are the
# negatives the model must get right, and they are exactly the ones non-max
# suppression will have to rank below the correct extent. A candidate
# overlapping nothing is a free win learned in a handful of trees.
#
# So near misses are kept in full and only the easy negatives are sampled.
# Uniform subsampling would discard both at the same rate, which is the one
# thing that must not happen.
# 0.25, not 0.1. At 0.1 almost every Background candidate qualifies — on a
# dense page nearly everything overlaps something — and the selection stopped
# selecting: 5.64M rows became 4.69M. A candidate matching a real region at IoU
# 0.25 to 0.5 is genuinely a near miss; one at 0.1 is just adjacent.
NEAR_MISS_IOU = 0.25
EASY_NEGATIVE_KEEP = 0.15

# Class weighting defaults to OFF, and that is a correction rather than a
# preference. Weighting by sqrt(background/count) upweights Table 4.5x and Title
# 45x, which is reasonable for balanced classification and wrong here: non-max
# suppression ranks candidates by probability, so distorting the probabilities
# distorts the ranking. Measured with weights on, the classifier called 44
# candidates per page Table against 0.73 real tables per page, and suppression
# halved recall on every structural class picking between them.

# The IoU head. Class probability alone cannot rank a table proposed one line
# short below the same table proposed correctly: their CONTENT features are
# nearly identical, so the classifier scores them nearly the same, and non-max
# suppression then picks between them almost at random. Measured, that halves
# recall on every structural class.
#
# So a second model predicts how well a candidate's extent matches whatever it
# overlaps, and suppression ranks by class probability TIMES predicted overlap.
# This needs no new data: the IoU is already in the joined dataset.
IOU_PARAMS = {
    "objective": "regression",
    "metric": "l1",
    "learning_rate": 0.08,
    "num_leaves": 63,
    "max_depth": 8,
    "min_data_in_leaf": 50,
    "lambda_l2": 10.0,
    "feature_fraction": 0.9,
    "bagging_fraction": 0.8,
    "bagging_freq": 1,
    "seed": SEED,
    "deterministic": True,
    "force_row_wise": True,
    "num_threads": 8,
    "verbosity": -1,
}

CHUNK = 400_000


def select_training_rows(path, scratch, rng):
    """Stream train rows through the sampler into scratch; return the count."""
    kept = 0
    for raw in open(path):
        row = json.loads(raw)
        if row["split"] != "train":
            continue
        if (row["label"] == "Background" and row["iou"] < NEAR_MISS_IOU
                and rng.random() > EASY_NEGATIVE_KEEP):
            continue
        scratch.write(raw)
        kept += 1
    scratch.flush()
    return kept


def read_matrix(path, count, width):
    """Read up to count rows into preallocated arrays."""
    x = np.zeros((count, width), dtype=np.float32)
    y = np.zeros(count, dtype=np.int32)
    overlap = np.zeros(count, dtype=np.float32)
    near = np.full(count, b"", dtype="S16")
    n = 0
    for raw in open(path):
        row = json.loads(raw)
        if len(row["f"]) != width:
            sys.exit(f"contract mismatch: data has {len(row['f'])} columns, "
                     f"the emitter defines {width}")
        x[n] = row["f"]
        y[n] = CLASS_INDEX[row["label"]]
        overlap[n] = row["iou"]
        near[n] = row.get("near", "").encode()
        n += 1
        if n == count:
            break
    return x[:n], y[:n], overlap[:n], near[:n]


def iter_val_chunks(path, width):
    """Yield (features, labels, iou) chunks of validation rows."""
    x = np.zeros((CHUNK, width), dtype=np.float32)
    y = np.zeros(CHUNK, dtype=np.int32)
    overlap = np.zeros(CHUNK, dtype=np.float32)
    n = 0
    for raw in open(path):
        row = json.loads(raw)
        if row["split"] != "val":
            continue
        x[n] = row["f"]
        y[n] = CLASS_INDEX[row["label"]]
        overlap[n] = row["iou"]
        n += 1
        if n == CHUNK:
            yield x, y, overlap
            n = 0
    if n:
        yield x[:n], y[:n], overlap[:n]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dataset", required=True)
    ap.add_argument("--model-out", required=True)
    ap.add_argument("--rounds", type=int, default=350)
    ap.add_argument("--binary", default="../../../bin/spike")
    ap.add_argument("--objective", choices=("class", "iou"), default="class",
                    help="class: which region is this. iou: how good is this extent.")
    ap.add_argument("--weights", choices=("none", "sqrt"), default="none",
                    help="class weighting; see the note above CLASS_WEIGHTS")
    args = ap.parse_args()

    names = json.loads(subprocess.run(
        [args.binary, "proposal-features"], capture_output=True, check=True, text=True).stdout)
    width = len(names)
    print(f"feature contract: {width} features, read from the Go binary")

    rng = np.random.default_rng(SEED)
    with tempfile.NamedTemporaryFile("w", suffix=".jsonl", delete=False,
                                     dir=os.path.dirname(args.dataset) or ".") as scratch:
        scratch_path = scratch.name
        count = select_training_rows(args.dataset, scratch, rng)
    print(f"selected {count:,} training rows "
          f"(all near misses, {EASY_NEGATIVE_KEEP:.0%} of easy negatives)")

    try:
        x, y, overlap, near = read_matrix(scratch_path, count, width)
        print(f"loaded {len(y):,} x {width} = {x.nbytes / 1e9:.2f} GB")

        counts = collections.Counter(y.tolist())
        print(f"\n{'class':16s} {'train':>10s} {'share':>8s}")
        for index, name in enumerate(CLASSES):
            print(f"{name:16s} {counts[index]:10d} {counts[index] / max(len(y), 1):7.2%}")

        weights = np.ones(len(y), dtype=np.float32)
        if args.objective == "iou" and near is not None:
            # The end-to-end diagnosis found the IoU head's errors concentrated
            # on same-class wrong-extent TABLES: where a table was outranked,
            # the head preferred the wrong extent 80% of the time. Tables are
            # 3.8% of training rows, so their extent-ranking mistakes are cheap
            # for the loss and expensive for the pipeline. Upweighting every
            # row whose nearest region is a table makes the head pay for them.
            weights[near == b"Table"] = 3.0
        if args.weights == "sqrt":
            background = counts[CLASS_INDEX["Background"]]
            for index in range(len(CLASSES)):
                if counts[index] > 0:
                    weights[y == index] = (background / counts[index]) ** 0.5

        if args.objective == "iou":
            params = dict(IOU_PARAMS)
            label = overlap
        else:
            params = dict(PARAMS)
            label = y

        dtrain = lgb.Dataset(x, label=label, weight=weights, feature_name=names)
        print(f"\ntraining {args.rounds} rounds, objective={args.objective}, "
              f"weights={args.weights}...")
        # No validation set is passed to LightGBM. It would bin and hold a second
        # copy of two million rows purely to print a loss, and validation is
        # scored properly below in chunks that never coexist.
        model = lgb.train(params, dtrain, num_boost_round=args.rounds,
                          callbacks=[lgb.log_evaluation(0)])
        del x, y, overlap, near, weights, label, dtrain
    finally:
        os.unlink(scratch_path)

    if args.objective == "iou":
        errors, count_val = [], 0
        for features, _, actual in iter_val_chunks(args.dataset, width):
            predicted = np.clip(model.predict(features), 0, 1)
            errors.append(np.abs(predicted - actual))
            count_val += len(actual)
        errors = np.concatenate(errors)
        print(f"\nvalidation over {count_val:,} proposals")
        print(f"  mean absolute IoU error {errors.mean():.4f}")
        print(f"  median                  {np.median(errors):.4f}")
        model.save_model(args.model_out)
        print(f"\nwrote {args.model_out}")
        gains = sorted(zip(names, model.feature_importance("gain")), key=lambda kv: -kv[1])
        print("\ntop features by gain:")
        for name, gain in gains[:15]:
            print(f"  {name:24s} {gain:12.0f}")
        return

    tp = collections.Counter()
    fp = collections.Counter()
    fn = collections.Counter()
    total = 0
    for features, actual, _ in iter_val_chunks(args.dataset, width):
        predicted = model.predict(features).argmax(axis=1)
        total += len(actual)
        for index in range(len(CLASSES)):
            tp[index] += int(np.sum((predicted == index) & (actual == index)))
            fp[index] += int(np.sum((predicted == index) & (actual != index)))
            fn[index] += int(np.sum((predicted != index) & (actual == index)))

    print(f"\nvalidation over {total:,} proposals")
    print(f"{'class':16s} {'support':>9s} {'prec':>7s} {'recall':>7s} {'F1':>7s}")
    print("-" * 51)
    for index, name in enumerate(CLASSES):
        precision = tp[index] / (tp[index] + fp[index]) if tp[index] + fp[index] else 0.0
        recall = tp[index] / (tp[index] + fn[index]) if tp[index] + fn[index] else 0.0
        f1 = 2 * precision * recall / (precision + recall) if precision + recall else 0.0
        print(f"{name:16s} {tp[index] + fn[index]:9d} {precision:7.3f} {recall:7.3f} {f1:7.3f}")

    model.save_model(args.model_out)
    with open(args.model_out + ".classes.json", "w") as handle:
        json.dump(CLASSES, handle)
    print(f"\nwrote {args.model_out}")

    gains = sorted(zip(names, model.feature_importance("gain")), key=lambda kv: -kv[1])
    print("\ntop features by gain:")
    for name, gain in gains[:15]:
        print(f"  {name:24s} {gain:12.0f}")


if __name__ == "__main__":
    main()
