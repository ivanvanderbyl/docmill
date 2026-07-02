#!/usr/bin/env python3
"""Materialise Hugging Face docling-dpbench rows for docmill benchmark runs."""

import argparse
import base64
import json
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Any, Dict, Iterable, List


DEFAULT_DATASET = "docling-project/docling-dpbench"
DEFAULT_SPLIT = "test"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--dataset", default=DEFAULT_DATASET)
    parser.add_argument("--config", default="default")
    parser.add_argument("--split", default=DEFAULT_SPLIT)
    parser.add_argument("--out", default="benchmarks/dpbench/corpus")
    parser.add_argument("--limit", type=int, default=200)
    parser.add_argument("--page-size", type=int, default=20)
    args = parser.parse_args()
    out = Path(args.out)
    pdf_dir = out / "pdf"
    gt_dir = out / "groundtruth"
    pdf_dir.mkdir(parents=True, exist_ok=True)
    gt_dir.mkdir(parents=True, exist_ok=True)

    cases = []  # type: List[Dict[str, Any]]
    for index, row in enumerate(fetch_rows(args.dataset, args.config, args.split, args.limit, args.page_size)):
        doc_id = safe_id(row.get("document_id") or row.get("document_filepath") or f"doc-{index:04d}")
        pdf_bytes = decode_binary_document(row.get("BinaryDocument"))
        ground_truth = ground_truth_markdown(row.get("GroundTruthDocument"))

        pdf_path = pdf_dir / f"{doc_id}.pdf"
        gt_path = gt_dir / f"{doc_id}.md"
        pdf_path.write_bytes(pdf_bytes)
        gt_path.write_text(ground_truth, encoding="utf-8")

        cases.append(
            {
                "id": doc_id,
                "pdf_path": str(pdf_path.relative_to(out)),
                "ground_truth_path": str(gt_path.relative_to(out)),
                "pages": page_count(row.get("GroundTruthDocument")),
            }
        )

    manifest = {
        "name": "dpbench",
        "source": f"https://huggingface.co/datasets/{args.dataset}",
        "cases": cases,
    }
    (out / "manifest.json").write_text(json.dumps(manifest, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print(f"wrote {len(cases)} cases to {out}")
    return 0


def fetch_rows(dataset: str, config: str, split: str, limit: int, page_size: int) -> Iterable[Dict[str, Any]]:
    if page_size <= 0:
        raise ValueError("--page-size must be positive")
    offset = 0
    yielded = 0
    while True:
        length = page_size
        if limit > 0:
            remaining = limit - yielded
            if remaining <= 0:
                return
            length = min(length, remaining)

        payload = fetch_rows_page(dataset, config, split, offset, length)
        rows = payload.get("rows") or []
        if not rows:
            return
        for item in rows:
            row = item.get("row") if isinstance(item, dict) else None
            if row is None:
                continue
            yield row
            yielded += 1
            if limit > 0 and yielded >= limit:
                return
        offset += len(rows)


def fetch_rows_page(dataset: str, config: str, split: str, offset: int, length: int, retries: int = 3) -> Dict[str, Any]:
    query = urllib.parse.urlencode(
        {
            "dataset": dataset,
            "config": config,
            "split": split,
            "offset": offset,
            "length": length,
        }
    )
    url = "https://datasets-server.huggingface.co/rows?" + query
    request = urllib.request.Request(url, headers={"User-Agent": "docmill-dpbench/1.0"})
    for attempt in range(retries + 1):
        try:
            with urllib.request.urlopen(request, timeout=120) as response:
                if response.status != 200:
                    raise RuntimeError("Hugging Face rows API returned HTTP %s" % response.status)
                return json.loads(response.read().decode("utf-8"))
        except urllib.error.HTTPError as exc:
            if exc.code < 500 or attempt >= retries:
                raise
            time.sleep(2 ** attempt)
    raise RuntimeError("Hugging Face rows API request failed")


def decode_binary_document(value: Any) -> bytes:
    if value is None:
        raise ValueError("BinaryDocument is empty")
    if isinstance(value, bytes):
        return value
    if isinstance(value, bytearray):
        return bytes(value)
    if not isinstance(value, str):
        raise TypeError(f"unsupported BinaryDocument type: {type(value)!r}")

    text = value.strip()
    if text.startswith("%PDF"):
        return text.encode("latin-1")
    if text.startswith("data:") and "," in text:
        text = text.split(",", 1)[1]
    try:
        decoded = base64.b64decode(text, validate=True)
    except Exception as exc:
        raise ValueError("BinaryDocument is not PDF text or base64") from exc
    if not decoded.startswith(b"%PDF"):
        raise ValueError("decoded BinaryDocument does not start with %PDF")
    return decoded


def ground_truth_markdown(value: Any) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        return str(value)
    text = value.strip()
    if not text:
        return ""
    if not text.startswith("{"):
        return text
    try:
        doc = json.loads(text)
    except json.JSONDecodeError:
        return text
    if isinstance(doc, dict) and doc.get("schema_name") == "DoclingDocument":
        return docling_document_to_markdown(doc)
    return text


def docling_document_to_markdown(doc: Dict[str, Any]) -> str:
    refs = {}  # type: Dict[str, Dict[str, Any]]
    for section in ("texts", "tables"):
        for item in doc.get(section, []) or []:
            ref = item.get("self_ref")
            if ref:
                refs[ref] = item

    body = doc.get("body") or {}
    parts = []  # type: List[str]
    for child in body.get("children", []) or []:
        ref = child.get("$ref")
        item = refs.get(ref)
        if not item:
            continue
        rendered = render_docling_item(item)
        if rendered:
            parts.append(rendered)
    return "\n\n".join(parts).strip() + "\n"


def render_docling_item(item: Dict[str, Any]) -> str:
    label = item.get("label")
    if label == "table":
        return render_table(item)
    text = str(item.get("text") or "").strip()
    if not text:
        return ""
    if label == "title":
        return "# " + text
    if label == "section_header":
        level = item.get("level")
        if not isinstance(level, int) or level < 1 or level > 6:
            level = 2
        return "#" * level + " " + text
    if label == "list_item":
        return "- " + text
    return text


def render_table(item: Dict[str, Any]) -> str:
    data = item.get("data") or {}
    cells = data.get("table_cells") or []
    if not cells:
        return ""
    rows = max(int(cell.get("end_row_offset_idx") or 0) for cell in cells)
    cols = max(int(cell.get("end_col_offset_idx") or 0) for cell in cells)
    grid = [["" for _ in range(cols)] for _ in range(rows)]
    for cell in cells:
        row = int(cell.get("start_row_offset_idx") or 0)
        col = int(cell.get("start_col_offset_idx") or 0)
        if row < rows and col < cols:
            grid[row][col] = markdown_cell(str(cell.get("text") or ""))
    lines = ["| " + " | ".join(row) + " |" for row in grid]
    if lines:
        lines.insert(1, "| " + " | ".join("---" for _ in range(cols)) + " |")
    return "\n".join(lines)


def markdown_cell(text: str) -> str:
    return " ".join(text.replace("|", "\\|").split())


def page_count(value: Any) -> int:
    if not isinstance(value, str) or not value.strip().startswith("{"):
        return 0
    try:
        doc = json.loads(value)
    except json.JSONDecodeError:
        return 0
    pages = set()
    for section in ("texts", "tables", "pictures"):
        for item in doc.get(section, []) or []:
            for prov in item.get("prov", []) or []:
                page = prov.get("page_no")
                if isinstance(page, int):
                    pages.add(page)
    return len(pages)


def safe_id(value: Any) -> str:
    text = str(value)
    text = Path(text).stem if "/" in text else text
    text = re.sub(r"[^A-Za-z0-9._-]+", "-", text).strip("-")
    return text or "document"


if __name__ == "__main__":
    raise SystemExit(main())
