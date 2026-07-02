#!/usr/bin/env python3
"""Visual side-by-side comparison of docmill vs other PDF->Markdown engines.

Two input modes, one interactive HTML report:

  RESULTS mode — load a DPBench results JSON and browse cases beside their
  ground truth and every engine's Markdown, with per-case scores:
      .../python benchmarks/compare_pages.py \
        --results benchmarks/dpbench/results/cached.json \
        --cases worst:reading_order_nid:20

  PAGES mode — render an arbitrary PDF's pages and run engines live on each
  (for documents with no ground truth, e.g. a large real-world PDF):
      .../python benchmarks/compare_pages.py \
        --pdf large-example.pdf --pages 11,25,55 \
        --tools docmill,pymupdf4llm,docling

The report has, in a sticky toolbar: a tick box per engine (and the page render
and ground truth) to show/hide its column, and a Rendered/Raw switch that flips
every cell between rendered Markdown and raw source. Markdown is rendered in the
browser by the vendored marked.js, so the report is a single self-contained file.

Run with the dpbench venv python (it needs pymupdf for page extraction and reaches
the competitor adapters):
  /private/tmp/dpbench-venv/bin/python benchmarks/compare_pages.py ...
"""
import argparse
import html
import json
import os
import subprocess
import sys

import pymupdf

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DOCMILL = os.path.join(REPO, "bin", "docmill-bench")
CONVERTER = os.path.join(REPO, "benchmarks", "dpbench", "python_converter.py")
CORPUS = os.path.join(REPO, "benchmarks", "dpbench", "corpus")
CACHE = os.path.join(REPO, "benchmarks", "dpbench", "cache")
MARKED = os.path.join(REPO, "benchmarks", "assets", "marked.min.js")
PYTHON = sys.executable  # the venv interpreter running this script

SCORE_KEYS = [
    ("extraction_accuracy", "ext"),
    ("reading_order_nid", "nid"),
    ("table_structure_teds", "teds"),
    ("heading_level_mhs", "mhs"),
]


# ------------------------------------------------------------------ engines ---
def run_docmill(pdf_path):
    r = subprocess.run([DOCMILL, "convert", pdf_path], capture_output=True, text=True)
    return r.stdout if r.returncode == 0 else f"(docmill error rc={r.returncode})\n{r.stderr[:400]}"


def run_competitor(tool, pdf_path, out_md):
    try:
        subprocess.run(
            [PYTHON, CONVERTER, "--tool", tool, pdf_path, out_md],
            capture_output=True, text=True, timeout=300,
        )
    except subprocess.TimeoutExpired:
        return "(timeout)"
    if os.path.exists(out_md):
        return open(out_md, encoding="utf-8", errors="replace").read()
    return "(no output)"


def cached_or_live(tool, case_id, pdf_path, workdir):
    """Markdown for a competitor: prefer the frozen DPBench cache, else run live."""
    cache_path = os.path.join(CACHE, tool.lower(), case_id + ".md")
    if os.path.exists(cache_path):
        return open(cache_path, encoding="utf-8", errors="replace").read()
    if pdf_path and os.path.exists(pdf_path):
        return run_competitor(tool.lower(), pdf_path, os.path.join(workdir, f"{tool}.md"))
    return "(no cached output)"


# -------------------------------------------------------------------- pages ---
def render_page(pdf, n, dpi, outdir, pad):
    prefix = os.path.join(outdir, "render")
    subprocess.run(
        ["pdftoppm", "-png", "-r", str(dpi), "-f", str(n), "-l", str(n), pdf, prefix],
        check=False, capture_output=True,
    )
    for width in (pad, pad + 1, 1, 2, 3, 4, 5, 6):
        cand = f"{prefix}-{n:0{width}d}.png"
        if os.path.exists(cand):
            final = os.path.join(outdir, f"page-{n:0{pad}d}.png")
            if cand != final:
                os.replace(cand, final)
            return os.path.basename(final)
    return None


def render_named(pdf, dpi, outdir, name):
    prefix = os.path.join(outdir, name)
    subprocess.run(
        ["pdftoppm", "-png", "-r", str(dpi), "-f", "1", "-l", "1", pdf, prefix],
        check=False, capture_output=True,
    )
    for cand in (f"{prefix}-1.png", f"{prefix}-01.png", f"{prefix}-001.png"):
        if os.path.exists(cand):
            final = os.path.join(outdir, name + ".png")
            os.replace(cand, final)
            return os.path.basename(final)
    return None


def extract_page_pdf(doc, n, path):
    out = pymupdf.open()
    out.insert_pdf(doc, from_page=n - 1, to_page=n - 1)
    out.save(path)
    out.close()


def parse_pages(spec, total):
    pages = set()
    for part in spec.split(","):
        part = part.strip()
        if not part:
            continue
        if "-" in part:
            a, b = part.split("-", 1)
            pages.update(range(int(a), int(b) + 1))
        else:
            pages.add(int(part))
    return sorted(p for p in pages if 1 <= p <= total)


# ----------------------------------------------------------------- selection ---
def select_cases(results, docmill_name, spec):
    """Resolve --cases for results mode into an ordered list of case ids."""
    by_name = {t["name"]: t for t in results["tools"]}
    docmill = by_name.get(docmill_name) or results["tools"][0]
    ids = [c["id"] for c in docmill["case_results"]]
    if not spec:
        return ids[:25]
    if spec.startswith("worst:") or spec.startswith("best:"):
        kind, _, rest = spec.partition(":")
        metric, _, n = rest.partition(":")
        n = int(n) if n else 20
        scored = [(c["scores"].get(metric, 0), c["id"]) for c in docmill["case_results"]]
        scored.sort(reverse=(kind == "best"))
        return [cid for _, cid in scored[:n]]
    if spec.isdigit():
        return ids[: int(spec)]
    # comma list of ids or short prefixes
    out = []
    for token in spec.split(","):
        token = token.strip()
        match = next((cid for cid in ids if token in cid), None)
        if match:
            out.append(match)
    return out


def scores_for(results, case_id):
    """{tool_name: {metric: value}} for a case, from the results JSON."""
    out = {}
    for t in results["tools"]:
        for c in t["case_results"]:
            if c["id"] == case_id:
                out[t["name"]] = c.get("scores", {})
                break
    return out


# ----------------------------------------------------------------------- html ---
def score_class(value):
    if value >= 0.9:
        return "s-hi"
    if value >= 0.6:
        return "s-mid"
    return "s-lo"


def scores_badge(scores):
    if not scores:
        return ""
    parts = []
    for key, short in SCORE_KEYS:
        if key in scores:
            v = scores[key]
            parts.append(f'<span class="sc {score_class(v)}">{short} {v:.2f}</span>')
    return '<span class="scores">' + "".join(parts) + "</span>"


def col_id(name):
    return "eng-" + "".join(ch if ch.isalnum() else "_" for ch in name)


def build_html(rows, engines, has_image, title, out_path):
    marked_js = open(MARKED, encoding="utf-8").read() if os.path.exists(MARKED) else ""

    # toolbar: page render toggle (if any), then one tick box per engine
    toggles = []
    if has_image:
        toggles.append('<label><input type="checkbox" class="t" data-col="col-page" checked> page</label>')
    for e in engines:
        toggles.append(
            f'<label><input type="checkbox" class="t" data-col="{col_id(e)}" checked> {html.escape(e)}</label>'
        )

    sections = []
    for row in rows:
        cells = []
        if has_image:
            img = (
                f'<img src="{row["img"]}" loading="lazy">' if row.get("img") else "(no render)"
            )
            cells.append(f'<div class="col col-page">{img}</div>')
        for e in engines:
            data = row["cells"].get(e)
            if data is None:
                cells.append(f'<div class="col {col_id(e)} missing"><div class="hd">{html.escape(e)}</div><div class="empty">—</div></div>')
                continue
            badge = scores_badge(data.get("scores"))
            raw = data.get("md", "")
            cells.append(
                f'<div class="col {col_id(e)}">'
                f'<div class="hd">{html.escape(e)}{badge}</div>'
                f'<div class="rendered"></div>'
                f'<pre class="raw">{html.escape(raw)}</pre>'
                f"</div>"
            )
        sections.append(
            f'<section><h2>{html.escape(str(row["label"]))}</h2>'
            f'<div class="row">{"".join(cells)}</div></section>'
        )

    style = """
    *{box-sizing:border-box}
    body{font:13px/1.45 -apple-system,Segoe UI,sans-serif;margin:0;background:#f4f4f5;color:#18181b}
    body.raw .rendered{display:none}
    body.raw .raw{display:block}
    body:not(.raw) .raw{display:none}
    body:not(.raw) .rendered{display:block}
    .col.hidden{display:none}
    #bar{position:sticky;top:0;z-index:5;background:#18181b;color:#fff;padding:8px 12px;display:flex;
         gap:14px;flex-wrap:wrap;align-items:center}
    #bar b{font-size:14px;margin-right:6px}
    #bar label{cursor:pointer;user-select:none;white-space:nowrap}
    #bar .mode{margin-left:auto;display:flex;gap:8px;align-items:center;background:#27272a;padding:4px 8px;border-radius:6px}
    h2{margin:0;padding:7px 12px;background:#3f3f46;color:#fff;font-size:13px;position:sticky;top:37px}
    .row{display:flex;gap:12px;padding:12px;align-items:flex-start;overflow-x:auto}
    .col{flex:0 0 380px;background:#fff;border:1px solid #d4d4d8;border-radius:7px;overflow:hidden;
         max-height:80vh;display:flex;flex-direction:column}
    .col-page{flex:0 0 auto}
    .col-page img{display:block;max-width:480px;border-radius:7px}
    .hd{font-weight:700;padding:6px 9px;background:#e4e4e7;border-bottom:1px solid #d4d4d8;
        position:sticky;top:0;display:flex;flex-wrap:wrap;gap:6px;align-items:center}
    .scores{display:flex;gap:4px;flex-wrap:wrap;font-weight:400}
    .sc{font-size:10px;padding:1px 5px;border-radius:4px;color:#fff}
    .s-hi{background:#16a34a}.s-mid{background:#d97706}.s-lo{background:#dc2626}
    .rendered,.raw{overflow:auto;padding:9px 11px;margin:0}
    .raw{white-space:pre-wrap;word-break:break-word;font:11px/1.4 ui-monospace,SFMono-Regular,monospace;background:#fafafa}
    .rendered{font-size:12px}
    .rendered h1{font-size:17px}.rendered h2{font-size:15px}.rendered h3{font-size:13.5px}
    .rendered h1,.rendered h2,.rendered h3,.rendered h4{margin:.5em 0 .3em;line-height:1.25}
    .rendered table{border-collapse:collapse;font-size:11px;margin:.4em 0}
    .rendered th,.rendered td{border:1px solid #c4c4c8;padding:2px 6px;text-align:left;vertical-align:top}
    .rendered th{background:#f1f1f3}
    .rendered code{background:#f1f1f3;padding:0 3px;border-radius:3px}
    .rendered pre{background:#f6f6f7;padding:8px;border-radius:5px;overflow:auto}
    .rendered img{max-width:100%}
    .missing .empty{padding:20px;color:#a1a1aa;text-align:center}
    section{border-bottom:3px solid #a1a1aa}
    """

    script = (
        marked_js
        + """
    (function(){
      var hasMarked = typeof marked !== 'undefined';
      if (hasMarked && marked.setOptions) marked.setOptions({gfm:true, breaks:false});
      function render(){
        document.querySelectorAll('.col .raw').forEach(function(pre){
          var tgt = pre.previousElementSibling;
          if (!tgt || !tgt.classList.contains('rendered') || tgt.dataset.done) return;
          tgt.innerHTML = hasMarked ? marked.parse(pre.textContent) : pre.textContent;
          tgt.dataset.done = '1';
        });
      }
      render();
      // engine / page tick boxes
      document.querySelectorAll('#bar .t').forEach(function(cb){
        cb.addEventListener('change', function(){
          document.querySelectorAll('.' + cb.dataset.col).forEach(function(el){
            el.classList.toggle('hidden', !cb.checked);
          });
        });
      });
      // rendered / raw switch
      document.querySelectorAll('input[name=mode]').forEach(function(r){
        r.addEventListener('change', function(){
          document.body.classList.toggle('raw', r.value === 'raw' && r.checked);
        });
      });
    })();
    """
    )

    doc = (
        "<!doctype html><meta charset=utf-8><title>"
        + html.escape(title)
        + "</title><style>"
        + style
        + "</style><body><div id=bar><b>"
        + html.escape(title)
        + "</b>"
        + "".join(toggles)
        + '<span class="mode"><label><input type="radio" name="mode" value="rendered" checked> rendered</label>'
        + '<label><input type="radio" name="mode" value="raw"> raw</label></span>'
        + "</div>"
        + "".join(sections)
        + "<script>"
        + script
        + "</script></body>"
    )
    with open(out_path, "w", encoding="utf-8") as f:
        f.write(doc)


# ------------------------------------------------------------------- modes ---
def run_results_mode(args, out):
    results = json.load(open(args.results, encoding="utf-8"))
    engines_all = [t["name"] for t in results["tools"]]
    engines = [e for e in (args.tools.split(",") if args.tools else engines_all) if e in engines_all]
    case_ids = select_cases(results, engines[0] if engines else "docmill", args.cases)
    columns = ["GT"] + engines

    rows = []
    for cid in case_ids:
        pdf_path = os.path.join(CORPUS, "pdf", cid + ".pdf")
        img = render_named(pdf_path, args.dpi, out, "img-" + col_id(cid)) if os.path.exists(pdf_path) else None
        per_scores = scores_for(results, cid)
        cells = {}
        gt_path = os.path.join(CORPUS, "groundtruth", cid + ".md")
        cells["GT"] = {"md": open(gt_path, encoding="utf-8", errors="replace").read() if os.path.exists(gt_path) else "(no ground truth)"}
        for e in engines:
            md = run_docmill(pdf_path) if e == "docmill" else cached_or_live(e, cid, pdf_path, out)
            cells[e] = {"md": md, "scores": per_scores.get(e)}
        short = cid.replace("doc_", "")[:14]
        rows.append({"label": short, "img": img, "cells": cells})
        print(f"case {short} …", file=sys.stderr)
    build_html(rows, columns, True, os.path.basename(args.results), os.path.join(out, "report.html"))


def run_pages_mode(args, out):
    doc = pymupdf.open(args.pdf)
    total = doc.page_count
    pad = len(str(total))
    pages = parse_pages(args.pages, total)
    engines = [t.strip() for t in args.tools.split(",") if t.strip()]
    for n in pages:
        print(f"page {n} …", file=sys.stderr)
        img = render_page(args.pdf, n, args.dpi, out, pad)
        page_pdf = os.path.join(out, f"src-{n:0{pad}d}.pdf")
        extract_page_pdf(doc, n, page_pdf)
        cells = {}
        for e in engines:
            md = run_docmill(page_pdf) if e == "docmill" else run_competitor(e, page_pdf, os.path.join(out, f"{e}-{n}.md"))
            cells[e] = {"md": md}
        rows_append = {"label": f"Page {n}", "img": img, "cells": cells}
        run_pages_mode.rows.append(rows_append)
    build_html(run_pages_mode.rows, engines, True, f"{os.path.basename(args.pdf)} — pages {args.pages}", os.path.join(out, "report.html"))


run_pages_mode.rows = []


def main():
    ap = argparse.ArgumentParser(description="Visual PDF->Markdown engine comparison report.")
    ap.add_argument("--results", help="DPBench results JSON (results mode)")
    ap.add_argument("--pdf", help="PDF to render and run engines on (pages mode)")
    ap.add_argument("--pages", default="1-5", help="pages mode: e.g. 6-12 or 8,10,40")
    ap.add_argument("--cases", default="", help="results mode: N | id,id | worst:METRIC[:N] | best:METRIC[:N]")
    ap.add_argument("--tools", default="", help="comma engine list (default: all in JSON / docmill,pymupdf4llm,docling)")
    ap.add_argument("--dpi", type=int, default=110)
    ap.add_argument("--out", default="/tmp/compare_report")
    args = ap.parse_args()

    os.makedirs(args.out, exist_ok=True)
    if args.results:
        run_results_mode(args, args.out)
    elif args.pdf:
        if not args.tools:
            args.tools = "docmill,pymupdf4llm,docling"
        run_pages_mode(args, args.out)
    else:
        ap.error("supply --results JSON or --pdf FILE")
    print(os.path.join(args.out, "report.html"))


if __name__ == "__main__":
    main()
