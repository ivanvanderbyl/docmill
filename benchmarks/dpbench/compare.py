#!/usr/bin/env python3
"""Compare two DPBench native-docmill result JSONs and print score deltas.

Usage: python3 benchmarks/dpbench/compare.py BASELINE.json CURRENT.json
"""
import json
import sys
from pathlib import Path


def native(path):
    data = json.loads(Path(path).read_text())
    return next(t for t in data["tools"] if t["name"] == "docmill")


def main():
    base = native(sys.argv[1])
    cur = native(sys.argv[2])
    fields = [
        ("cases", lambda t: t["cases"]),
        ("errors", lambda t: t["errors"]),
        ("milliseconds_per_page", lambda t: round(t.get("milliseconds_per_page", 0), 2)),
        ("extraction_accuracy", lambda t: round(t["scores"]["extraction_accuracy"], 4)),
        ("reading_order_nid", lambda t: round(t["scores"]["reading_order_nid"], 4)),
        ("table_structure_teds", lambda t: round(t["scores"]["table_structure_teds"], 4)),
        ("heading_level_mhs", lambda t: round(t["scores"]["heading_level_mhs"], 4)),
    ]
    regressed = []
    print(f"{'metric':24} {'baseline':>12} {'current':>12} {'delta':>10}")
    for name, get in fields:
        b, c = get(base), get(cur)
        delta = round(c - b, 4) if isinstance(b, (int, float)) else "-"
        flag = ""
        if name in ("extraction_accuracy", "reading_order_nid", "table_structure_teds", "heading_level_mhs"):
            if isinstance(delta, (int, float)) and delta < -0.0005:
                flag = "  <-- REGRESSION"
                regressed.append(name)
        print(f"{name:24} {b:>12} {c:>12} {str(delta):>10}{flag}")
    print()
    if regressed:
        print("RESULT: REGRESSION in " + ", ".join(regressed))
        sys.exit(1)
    print("RESULT: NO_REGRESSION")


if __name__ == "__main__":
    main()
