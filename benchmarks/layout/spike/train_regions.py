#!/usr/bin/env python3
"""Train the REGION model: does this candidate region stand?

The second stage of the cascade. The line model proposes runs of same-label
lines; this decides which survive. Rejected candidates fall back to their line
labels, so a rejection costs nothing beyond the region itself.

Trained over every proposed class, not just Table and Picture. The plan scopes
the gate to the structural classes, but the features are class-agnostic and the
label distribution inside a candidate is itself a feature, so one model covers
all of them and the caller applies it only where it wants a gate.
"""

import argparse, collections, json, subprocess, sys
import lightgbm as lgb, numpy as np

SEED = 20260806
PARAMS = {
    "objective": "binary", "learning_rate": 0.05, "num_leaves": 31, "max_depth": 6,
    "min_data_in_leaf": 20, "feature_fraction": 0.9, "bagging_fraction": 0.9,
    "bagging_freq": 1, "seed": SEED, "deterministic": True, "force_row_wise": True,
    "num_threads": 4, "verbosity": -1,
}

def load(path, names):
    splits = collections.defaultdict(lambda: {"x": [], "y": [], "c": []})
    with open(path) as handle:
        for raw in handle:
            r = json.loads(raw)
            b = splits[r["split"]]
            b["x"].append(r["f"]); b["y"].append(r["label"]); b["c"].append(r["class"])
    out = {}
    for split, b in splits.items():
        x = np.array(b["x"], dtype=np.float64)
        if x.shape[1] != len(names):
            sys.exit(f"contract mismatch: data {x.shape[1]} cols, emitter {len(names)}")
        out[split] = (x, np.array(b["y"], dtype=np.int32), np.array(b["c"]))
    return out

def scores(y, p):
    tp = int(np.sum((p == 1) & (y == 1))); fp = int(np.sum((p == 1) & (y == 0)))
    fn = int(np.sum((p == 0) & (y == 1)))
    pr = tp / (tp + fp) if tp + fp else 0.0
    rc = tp / (tp + fn) if tp + fn else 0.0
    return pr, rc, (2 * pr * rc / (pr + rc) if pr + rc else 0.0)

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dataset", required=True)
    ap.add_argument("--model-out", required=True)
    ap.add_argument("--rounds", type=int, default=300)
    ap.add_argument("--binary", default="../../../bin/spike")
    a = ap.parse_args()

    names = json.loads(subprocess.run([a.binary, "regions", "-features"], capture_output=True, check=True, text=True).stdout)
    splits = load(a.dataset, names)
    tx, ty, _ = splits["train"]; vx, vy, vc = splits["val"]
    print(f"train {len(ty)} candidates ({ty.mean():.1%} accepted)")
    print(f"val   {len(vy)} candidates ({vy.mean():.1%} accepted)")

    booster = lgb.train(PARAMS, lgb.Dataset(tx, label=ty, feature_name=names), num_boost_round=a.rounds)
    p = booster.predict(vx); pred = (p >= 0.5).astype(int)

    pr, rc, f1 = scores(vy, pred)
    print(f"\n=== accept/reject, all candidates (DocLayNet val) ===")
    print(f"  precision {pr:.4f}  recall {rc:.4f}  F1 {f1:.4f}")
    print(f"\n{'proposed class':16s} {'cands':>8s} {'true':>7s} {'prec':>7s} {'recall':>7s} {'F1':>7s}")
    for cls in sorted(set(vc)):
        m = vc == cls
        if m.sum() < 50: continue
        cpr, crc, cf1 = scores(vy[m], pred[m])
        print(f"{cls:16s} {int(m.sum()):8d} {int(vy[m].sum()):7d} {cpr:7.3f} {crc:7.3f} {cf1:7.3f}")

    booster.save_model(a.model_out)
    print(f"\nwrote {a.model_out}: {booster.num_trees()} trees")
    print("\ntop features by gain:")
    for n, g in sorted(zip(names, booster.feature_importance("gain")), key=lambda kv: -kv[1])[:12]:
        print(f"  {n:26s} {g:14.1f}")

if __name__ == "__main__":
    main()
