#!/usr/bin/env python3

import base64
import json
import unittest

import materialize


class MaterializeTest(unittest.TestCase):
    def test_decode_binary_document_accepts_base64_pdf(self):
        encoded = base64.b64encode(b"%PDF-test").decode("ascii")

        self.assertEqual(materialize.decode_binary_document(encoded), b"%PDF-test")

    def test_docling_document_to_markdown_uses_heading_level_and_tables(self):
        doc = {
            "schema_name": "DoclingDocument",
            "body": {
                "children": [
                    {"$ref": "#/texts/0"},
                    {"$ref": "#/texts/1"},
                    {"$ref": "#/tables/0"},
                ]
            },
            "texts": [
                {"self_ref": "#/texts/0", "label": "section_header", "level": 1, "text": "Method"},
                {"self_ref": "#/texts/1", "label": "text", "text": "Body text"},
            ],
            "tables": [
                {
                    "self_ref": "#/tables/0",
                    "label": "table",
                    "data": {
                        "table_cells": [
                            {"start_row_offset_idx": 0, "end_row_offset_idx": 1, "start_col_offset_idx": 0, "end_col_offset_idx": 1, "text": "A"},
                            {"start_row_offset_idx": 0, "end_row_offset_idx": 1, "start_col_offset_idx": 1, "end_col_offset_idx": 2, "text": "B"},
                            {"start_row_offset_idx": 1, "end_row_offset_idx": 2, "start_col_offset_idx": 0, "end_col_offset_idx": 1, "text": "1"},
                            {"start_row_offset_idx": 1, "end_row_offset_idx": 2, "start_col_offset_idx": 1, "end_col_offset_idx": 2, "text": "2"},
                        ]
                    },
                }
            ],
        }

        markdown = materialize.ground_truth_markdown(json.dumps(doc))

        self.assertTrue(markdown.startswith("# Method\n\n"), markdown)
        self.assertIn("Body text", markdown)
        self.assertIn("| A | B |", markdown)
        self.assertIn("| --- | --- |", markdown)
        self.assertIn("| 1 | 2 |", markdown)


if __name__ == "__main__":
    unittest.main()
