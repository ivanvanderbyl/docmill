#!/usr/bin/env python3
"""Small adapter layer for Python PDF-to-Markdown libraries."""

import argparse
import sys
from pathlib import Path

try:
    import importlib.metadata as importlib_metadata
except ImportError:  # Python 3.6/3.7 without importlib_metadata backport.
    importlib_metadata = None


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--tool",
        required=True,
        choices=["docling", "markitdown", "pymupdf4llm", "opendataloader", "liteparse", "pypdf"],
    )
    parser.add_argument("--version", action="store_true")
    parser.add_argument("input", nargs="?")
    parser.add_argument("output", nargs="?")
    args = parser.parse_args()

    if args.version:
        print(package_version(args.tool))
        return 0
    if not args.input or not args.output:
        parser.error("input and output are required unless --version is set")

    input_path = Path(args.input)
    output_path = Path(args.output)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(convert(args.tool, input_path), encoding="utf-8")
    return 0


def package_version(tool: str) -> str:
    package = {
        "docling": "docling",
        "markitdown": "markitdown",
        "pymupdf4llm": "pymupdf4llm",
        "opendataloader": "opendataloader-pdf",
        "liteparse": "liteparse",
        "pypdf": "pypdf",
    }[tool]
    module = {
        "docling": "docling",
        "markitdown": "markitdown",
        "pymupdf4llm": "pymupdf4llm",
        "opendataloader": "opendataloader_pdf",
        "liteparse": "liteparse",
        "pypdf": "pypdf",
    }[tool]
    if importlib_metadata is not None:
        try:
            return importlib_metadata.version(package)
        except importlib_metadata.PackageNotFoundError:
            return "not-installed"
    try:
        module = __import__(module)
    except ImportError:
        return "not-installed"
    return str(getattr(module, "__version__", "unknown"))


def convert(tool: str, input_path: Path) -> str:
    if tool == "docling":
        try:
            from docling.document_converter import DocumentConverter
        except ImportError as exc:
            raise SystemExit("install docling to use the docling adapter") from exc
        result = DocumentConverter().convert(str(input_path))
        return result.document.export_to_markdown()

    if tool == "markitdown":
        try:
            from markitdown import MarkItDown
        except ImportError as exc:
            raise SystemExit("install markitdown to use the markitdown adapter") from exc
        result = MarkItDown().convert(str(input_path))
        return getattr(result, "text_content", str(result))

    if tool == "pymupdf4llm":
        try:
            import pymupdf4llm
        except ImportError as exc:
            raise SystemExit("install pymupdf4llm to use the pymupdf4llm adapter") from exc
        return pymupdf4llm.to_markdown(str(input_path))

    if tool == "liteparse":
        try:
            from liteparse import LiteParse
        except ImportError as exc:
            raise SystemExit("install liteparse to use the liteparse adapter") from exc
        parser = LiteParse(output_format="markdown", image_mode="placeholder", extract_links=True, quiet=True)
        return parser.parse(str(input_path)).text

    if tool == "pypdf":
        try:
            from pypdf import PdfReader
        except ImportError as exc:
            raise SystemExit("install pypdf to use the pypdf adapter") from exc
        reader = PdfReader(str(input_path))
        return "\n\n".join(page.extract_text() or "" for page in reader.pages)

    raise SystemExit(f"unsupported tool: {tool}")


if __name__ == "__main__":
    raise SystemExit(main())
