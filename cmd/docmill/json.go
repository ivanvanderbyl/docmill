package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	md "github.com/ivanvanderbyl/markdown"

	"github.com/ivanvanderbyl/docmill/pkg/render"
	"github.com/ivanvanderbyl/docmill/pkg/table"
)

type document struct {
	Items []documentItem `json:"items"`
}

type documentItem struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	OTSL string `json:"otsl,omitempty"`
}

// runJSON reads a JSON document representation from stdin and writes Markdown to
// stdout. Supported item types are "paragraph" (with "text") and "table" (with
// an "otsl" string).
func runJSON(stdin io.Reader, stdout, stderr io.Writer) error {
	var doc document
	if err := json.NewDecoder(stdin).Decode(&doc); err != nil {
		_, _ = fmt.Fprintf(stderr, "invalid document JSON: %v\n", err)
		return err
	}

	parts := make([]string, 0, len(doc.Items))
	for index, item := range doc.Items {
		switch strings.ToLower(item.Type) {
		case "paragraph":
			text, err := renderParagraph(item.Text)
			if err != nil {
				_, _ = fmt.Fprintf(stderr, "render paragraph %d: %v\n", index, err)
				return err
			}
			if text != "" {
				parts = append(parts, text)
			}
		case "table":
			text, err := render.Table(table.ParseOTSL(item.OTSL).Data())
			if err != nil {
				_, _ = fmt.Fprintf(stderr, "render table %d: %v\n", index, err)
				return err
			}
			if text != "" {
				parts = append(parts, text)
			}
		default:
			err := fmt.Errorf("unsupported item type %q at index %d", item.Type, index)
			_, _ = fmt.Fprintln(stderr, err)
			return err
		}
	}

	_, err := fmt.Fprint(stdout, strings.Join(parts, "\n\n"))
	return err
}

func renderParagraph(text string) (string, error) {
	var buf bytes.Buffer
	builder := md.NewMarkdown(&buf)
	builder.PlainText(text)
	if err := builder.Build(); err != nil {
		return "", err
	}
	return buf.String(), nil
}
