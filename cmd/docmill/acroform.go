package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/ivanvanderbyl/docmill/pkg/forms"
	"github.com/ivanvanderbyl/docmill/pkg/parser"
)

type acroformValueFile struct {
	Fields map[string]string `json:"fields"`
}

func runForms(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	args = stripArgSeparator(args)
	if len(args) < 1 {
		err := fmt.Errorf("usage: docmill forms <export|fill> ...")
		_, _ = fmt.Fprintln(stderr, err)
		return err
	}

	switch args[0] {
	case "export":
		return runFormsExport(ctx, args[1:], stdout, stderr)
	case "fill":
		return runFormsFill(ctx, args[1:], stdin, stdout, stderr)
	case "layout":
		return runFormsLayout(ctx, args[1:], stdout, stderr)
	default:
		err := fmt.Errorf("unknown forms command %q", args[0])
		_, _ = fmt.Fprintln(stderr, err)
		return err
	}
}

// formsLayoutFile is the docmill forms layout JSON shape: every AcroForm
// widget's bounding box and labelling, grouped by page. Boxes are in page
// points with a top-left origin (y grows downward).
type formsLayoutFile struct {
	Pages []formsLayoutPage `json:"pages"`
}

type formsLayoutPage struct {
	Page   int                `json:"page"` // 1-based
	Width  float64            `json:"width"`
	Height float64            `json:"height"`
	Fields []formsLayoutField `json:"fields"`
	// Groups lists detected multi-widget structure: "question" bands,
	// "field" groups (widgets sharing one AcroForm field, e.g. a Yes/No
	// checkbox pair), and "cluster" groups (adjacent widgets sharing one
	// caption). Members reference indices into Fields on this page.
	Groups []formsLayoutGroup `json:"groups,omitempty"`
}

type formsLayoutGroup struct {
	Kind   string `json:"kind"`
	Label  string `json:"label,omitempty"` // question text or shared caption
	Name   string `json:"name,omitempty"`  // field groups: the shared field name
	Fields []int  `json:"fields"`
}

type formsLayoutField struct {
	Name string `json:"name"`
	// Label is the resolved human-readable label: the authored /TU tooltip
	// when present, otherwise the detected page-text caption (pkg/forms).
	// LabelSource records which ("tooltip", "caption-left", "caption-below",
	// "state", ...); GroupLabel carries the enclosing question context when
	// the page uses numbered questions.
	Label       string         `json:"label,omitempty"`
	LabelSource string         `json:"labelSource,omitempty"`
	GroupLabel  string         `json:"groupLabel,omitempty"`
	Type        string         `json:"type,omitempty"`
	Value       string         `json:"value,omitempty"`
	OnState     string         `json:"onState,omitempty"` // checkbox/radio checked-state name
	Box         formsLayoutBox `json:"box"`
}

type formsLayoutBox struct {
	Left   float64 `json:"left"`
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
}

func runFormsLayout(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) != 1 {
		err := fmt.Errorf("usage: docmill forms layout <input.pdf>")
		_, _ = fmt.Fprintln(stderr, err)
		return err
	}

	doc, closeDoc, err := openNativePDFDocument(ctx, args[0])
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "open PDF: %v\n", err)
		return err
	}
	defer closeDoc()

	layout, err := collectFormsLayout(ctx, doc)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "read AcroForm layout: %v\n", err)
		return err
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(layout)
}

// collectFormsLayout walks every page and gathers its AcroForm widget boxes.
// Pages without form fields are omitted.
func collectFormsLayout(ctx context.Context, doc *parser.Document) (formsLayoutFile, error) {
	layout := formsLayoutFile{Pages: []formsLayoutPage{}}
	count, err := doc.PageCount(ctx)
	if err != nil {
		return layout, err
	}
	for i := range count {
		pg, err := doc.Page(ctx, i)
		if err != nil {
			return layout, fmt.Errorf("page %d: %w", i+1, err)
		}
		nativePage, ok := pg.(*parser.Page)
		if !ok {
			return layout, fmt.Errorf("page %d: native page unavailable", i+1)
		}
		boxes, err := nativePage.FormFieldBoxes(ctx)
		if err != nil {
			return layout, fmt.Errorf("page %d: %w", i+1, err)
		}
		if len(boxes) == 0 {
			continue
		}
		size, err := nativePage.Size(ctx)
		if err != nil {
			return layout, fmt.Errorf("page %d: %w", i+1, err)
		}
		lines, err := nativePage.TextCells(ctx)
		if err != nil {
			return layout, fmt.Errorf("page %d: %w", i+1, err)
		}
		words, err := nativePage.WordTextCells(ctx)
		if err != nil {
			return layout, fmt.Errorf("page %d: %w", i+1, err)
		}

		widgets := make([]forms.Widget, len(boxes))
		for j, b := range boxes {
			widgets[j] = forms.Widget{
				Name:    b.Name,
				Tooltip: b.Label,
				Type:    b.Type,
				OnState: b.OnState,
				Flags:   b.Flags,
				Box:     b.Box,
			}
		}
		detected := forms.Detect(widgets, lines, words)

		page := formsLayoutPage{
			Page:   i + 1,
			Width:  roundCoord(size.Width),
			Height: roundCoord(size.Height),
			Fields: make([]formsLayoutField, 0, len(boxes)),
		}
		for j, b := range boxes {
			label := detected.Labels[j]
			page.Fields = append(page.Fields, formsLayoutField{
				Name:        b.Name,
				Label:       label.Text,
				LabelSource: string(label.Source),
				GroupLabel:  label.Group,
				Type:        b.Type,
				Value:       b.Value,
				OnState:     b.OnState,
				Box: formsLayoutBox{
					Left:   roundCoord(b.Box.L),
					Top:    roundCoord(b.Box.T),
					Right:  roundCoord(b.Box.R),
					Bottom: roundCoord(b.Box.B),
				},
			})
		}
		for _, g := range detected.Groups {
			page.Groups = append(page.Groups, formsLayoutGroup{
				Kind:   string(g.Kind),
				Label:  g.Label,
				Name:   g.Name,
				Fields: g.Fields,
			})
		}
		layout.Pages = append(layout.Pages, page)
	}
	return layout, nil
}

// roundCoord rounds a page coordinate to 3 decimal places (0.001pt) for the
// JSON output: the engine stores PDF coordinates as float32, and widening to
// float64 otherwise leaks representation noise (57.25699996948242) with no
// real precision behind it.
func roundCoord(v float64) float64 { return math.Round(v*1000) / 1000 }

func runFormsExport(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) != 1 {
		err := fmt.Errorf("usage: docmill forms export <input.pdf>")
		_, _ = fmt.Fprintln(stderr, err)
		return err
	}

	doc, closeDoc, err := openNativePDFDocument(ctx, args[0])
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "open PDF: %v\n", err)
		return err
	}
	defer closeDoc()

	values, err := doc.AcroFormValues(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "read AcroForm: %v\n", err)
		return err
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(acroformValueFile{Fields: values})
}

func runFormsFill(ctx context.Context, args []string, stdin io.Reader, _ io.Writer, stderr io.Writer) error {
	if len(args) < 2 || len(args) > 3 {
		err := fmt.Errorf("usage: docmill forms fill <input.pdf> <output.pdf> [values.json]")
		_, _ = fmt.Fprintln(stderr, err)
		return err
	}

	valueInput := stdin
	if len(args) == 3 {
		file, err := os.Open(args[2])
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "open AcroForm values: %v\n", err)
			return err
		}
		defer file.Close()
		valueInput = file
	}

	values, err := decodeAcroFormValues(valueInput)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "read AcroForm values: %v\n", err)
		return err
	}

	doc, closeDoc, err := openNativePDFDocument(ctx, args[0])
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "open PDF: %v\n", err)
		return err
	}
	defer closeDoc()

	if _, err := doc.SetAcroFormValues(ctx, values); err != nil {
		_, _ = fmt.Fprintf(stderr, "fill AcroForm: %v\n", err)
		return err
	}

	out, err := os.Create(args[1])
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "create PDF: %v\n", err)
		return err
	}
	writeErr := doc.WritePDF(ctx, out)
	closeErr := out.Close()
	if writeErr != nil {
		_, _ = fmt.Fprintf(stderr, "write PDF: %v\n", writeErr)
		return writeErr
	}
	if closeErr != nil {
		_, _ = fmt.Fprintf(stderr, "close PDF: %v\n", closeErr)
		return closeErr
	}
	return nil
}

func openNativePDFDocument(ctx context.Context, path string) (*parser.Document, func(), error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, func() {}, fmt.Errorf("read PDF: %w", err)
	}

	backend := parser.NewBackend()
	doc, err := backend.OpenBytes(ctx, data)
	if err != nil {
		_ = backend.Close()
		return nil, func() {}, err
	}

	nativeDoc, ok := doc.(*parser.Document)
	if !ok {
		_ = doc.Close()
		_ = backend.Close()
		return nil, func() {}, fmt.Errorf("native PDFium document unavailable")
	}
	return nativeDoc, func() {
		_ = doc.Close()
		_ = backend.Close()
	}, nil
}

func decodeAcroFormValues(r io.Reader) (map[string]string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil, fmt.Errorf("empty JSON")
	}

	var wrapped acroformValueFile
	if err := json.Unmarshal(data, &wrapped); err == nil && wrapped.Fields != nil {
		return wrapped.Fields, nil
	}

	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}
