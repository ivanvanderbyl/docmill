#!/usr/bin/env python3

import sys
import types
import unittest
from pathlib import Path

import python_converter


class PythonConverterTest(unittest.TestCase):
    def test_opendataloader_version_falls_back_to_python_module(self):
        fake_module = types.ModuleType("opendataloader_pdf")
        fake_module.__version__ = "2.4.7-test"
        previous_module = sys.modules.get("opendataloader_pdf")
        previous_metadata = python_converter.importlib_metadata
        sys.modules["opendataloader_pdf"] = fake_module
        python_converter.importlib_metadata = None
        try:
            version = python_converter.package_version("opendataloader")
        finally:
            python_converter.importlib_metadata = previous_metadata
            if previous_module is None:
                del sys.modules["opendataloader_pdf"]
            else:
                sys.modules["opendataloader_pdf"] = previous_module

        self.assertEqual(version, "2.4.7-test")

    def test_liteparse_adapter_requests_markdown_output(self):
        captured = {}

        class FakeLiteParse:
            def __init__(self, **kwargs):
                captured["kwargs"] = kwargs

            def parse(self, input_path):
                captured["input_path"] = input_path
                return types.SimpleNamespace(text="liteparse markdown")

        fake_module = types.ModuleType("liteparse")
        fake_module.LiteParse = FakeLiteParse
        previous = sys.modules.get("liteparse")
        sys.modules["liteparse"] = fake_module
        try:
            markdown = python_converter.convert("liteparse", Path("sample.pdf"))
        finally:
            if previous is None:
                del sys.modules["liteparse"]
            else:
                sys.modules["liteparse"] = previous

        self.assertEqual(markdown, "liteparse markdown")
        self.assertEqual(captured["input_path"], "sample.pdf")
        self.assertEqual(captured["kwargs"]["output_format"], "markdown")
        self.assertEqual(captured["kwargs"]["image_mode"], "placeholder")
        self.assertTrue(captured["kwargs"]["extract_links"])
        self.assertTrue(captured["kwargs"]["quiet"])


if __name__ == "__main__":
    unittest.main()
