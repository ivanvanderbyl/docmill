package docmill_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	docmill "github.com/ivanvanderbyl/docmill"
)

// setupPDFium initialises a pdfium instance for testing.
func setupPDFium(t *testing.T) pdfium.Pdfium {
	t.Helper()

	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:  1,
		MaxIdle:  1,
		MaxTotal: 1,
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		pool.Close()
	})

	instance, err := pool.GetInstance(time.Second * 30)
	require.NoError(t, err)

	return instance
}

func TestConverter_ConvertBytes(t *testing.T) {
	instance := setupPDFium(t)
	converter := docmill.NewConverter(instance)

	// Create a simple test PDF
	testPDFPath := filepath.Join("testdata", "simple.pdf")
	if _, err := os.Stat(testPDFPath); os.IsNotExist(err) {
		t.Skip("Test PDF not found, skipping test")
		return
	}

	pdfBytes, err := os.ReadFile(testPDFPath)
	require.NoError(t, err)

	markdown, err := converter.ConvertBytes(pdfBytes)
	require.NoError(t, err)
	assert.NotEmpty(t, markdown)

	t.Logf("Generated markdown:\n%s", markdown)
}

func TestConverter_ConvertFile(t *testing.T) {
	instance := setupPDFium(t)
	converter := docmill.NewConverter(instance)

	testPDFPath := filepath.Join("testdata", "simple.pdf")
	if _, err := os.Stat(testPDFPath); os.IsNotExist(err) {
		t.Skip("Test PDF not found, skipping test")
		return
	}

	markdown, err := converter.ConvertFile(testPDFPath)
	require.NoError(t, err)
	assert.NotEmpty(t, markdown)
}

func TestConverter_GetDocumentInfo(t *testing.T) {
	instance := setupPDFium(t)
	converter := docmill.NewConverter(instance)

	testPDFPath := filepath.Join("testdata", "simple.pdf")
	if _, err := os.Stat(testPDFPath); os.IsNotExist(err) {
		t.Skip("Test PDF not found, skipping test")
		return
	}

	info, err := converter.GetDocumentInfo(testPDFPath)
	require.NoError(t, err)
	assert.Greater(t, info.PageCount, 0)
}

func TestConverter_ConvertPageRange(t *testing.T) {
	instance := setupPDFium(t)
	converter := docmill.NewConverter(instance)

	testPDFPath := filepath.Join("testdata", "multi_page.pdf")
	if _, err := os.Stat(testPDFPath); os.IsNotExist(err) {
		t.Skip("Test PDF not found, skipping test")
		return
	}

	// Convert pages 0-1 (first two pages)
	markdown, err := converter.ConvertPageRange(testPDFPath, 0, 1)
	require.NoError(t, err)
	assert.NotEmpty(t, markdown)

	// Should contain page separator
	assert.Contains(t, markdown, "---")
}

func TestEnrichedWord_IsBulletOrNumber(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected bool
	}{
		{"bullet dot", "•", true},
		{"bullet dash", "-", true},
		{"bullet asterisk", "*", true},
		{"numbered list", "1.", true},
		{"numbered list paren", "2)", true},
		{"regular word", "Hello", false},
		{"empty", "", false},
		{"number without punct", "5", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			word := docmill.EnrichedWord{Text: tt.text}
			assert.Equal(t, tt.expected, word.IsBulletOrNumber())
		})
	}
}

func TestParagraph_Text(t *testing.T) {
	para := docmill.Paragraph{
		Lines: []docmill.Line{
			{
				Words: []docmill.EnrichedWord{
					{Text: "Hello"},
					{Text: "World"},
				},
			},
			{
				Words: []docmill.EnrichedWord{
					{Text: "Second"},
					{Text: "Line"},
				},
			},
		},
	}

	expected := "Hello World\nSecond Line"
	assert.Equal(t, expected, para.Text())
}

func TestDocument_ToMarkdown_Headings(t *testing.T) {
	doc := &docmill.Document{
		Pages: []docmill.Page{
			{
				Number: 1,
				Paragraphs: []docmill.Paragraph{
					{
						Lines: []docmill.Line{
							{
								Words: []docmill.EnrichedWord{
									{Text: "Main", FontSize: 24, IsBold: true},
									{Text: "Heading", FontSize: 24, IsBold: true},
								},
							},
						},
						IsHeading:    true,
						HeadingLevel: 1,
					},
					{
						Lines: []docmill.Line{
							{
								Words: []docmill.EnrichedWord{
									{Text: "Some", FontSize: 12},
									{Text: "text", FontSize: 12},
								},
							},
						},
					},
				},
			},
		},
	}

	markdown := doc.ToMarkdown(docmill.DefaultConfig())
	assert.Contains(t, markdown, "# Main Heading")
	assert.Contains(t, markdown, "Some text")
}

func TestDocument_ToMarkdown_Lists(t *testing.T) {
	doc := &docmill.Document{
		Pages: []docmill.Page{
			{
				Number: 1,
				Paragraphs: []docmill.Paragraph{
					{
						Lines: []docmill.Line{
							{
								Words: []docmill.EnrichedWord{
									{Text: "•"},
									{Text: "First"},
									{Text: "item"},
								},
							},
						},
						IsList: true,
					},
					{
						Lines: []docmill.Line{
							{
								Words: []docmill.EnrichedWord{
									{Text: "•"},
									{Text: "Second"},
									{Text: "item"},
								},
							},
						},
						IsList: true,
					},
				},
			},
		},
	}

	markdown := doc.ToMarkdown(docmill.DefaultConfig())
	lines := strings.Split(strings.TrimSpace(markdown), "\n")

	// Each list item should be on its own line
	assert.GreaterOrEqual(t, len(lines), 2)
	assert.Contains(t, markdown, "•")
}

func TestDocument_ToMarkdown_CodeBlocks(t *testing.T) {
	doc := &docmill.Document{
		Pages: []docmill.Page{
			{
				Number: 1,
				Paragraphs: []docmill.Paragraph{
					{
						Lines: []docmill.Line{
							{
								Words: []docmill.EnrichedWord{
									{Text: "func", IsMonospace: true},
									{Text: "main()", IsMonospace: true},
								},
							},
						},
						IsCode: true,
					},
				},
			},
		},
	}

	markdown := doc.ToMarkdown(docmill.DefaultConfig())
	assert.Contains(t, markdown, "```")
	assert.Contains(t, markdown, "func main()")
}

func TestDocument_ToMarkdown_InlineFormatting(t *testing.T) {
	doc := &docmill.Document{
		Pages: []docmill.Page{
			{
				Number: 1,
				Paragraphs: []docmill.Paragraph{
					{
						Lines: []docmill.Line{
							{
								Words: []docmill.EnrichedWord{
									{Text: "This", IsBold: false},
									{Text: "is", IsBold: false},
									{Text: "bold", IsBold: true},
									{Text: "and", IsBold: false},
									{Text: "italic", IsItalic: true},
									{Text: "text", IsBold: false},
								},
							},
						},
					},
				},
			},
		},
	}

	markdown := doc.ToMarkdown(docmill.DefaultConfig())
	assert.Contains(t, markdown, "**bold**")
	assert.Contains(t, markdown, "*italic*")
	assert.Contains(t, markdown, "This is")
}

func TestRect_Methods(t *testing.T) {
	rect := docmill.Rect{
		X0: 10,
		Y0: 20,
		X1: 50,
		Y1: 60,
	}

	assert.Equal(t, 40.0, rect.Width())
	assert.Equal(t, 40.0, rect.Height())
	assert.Equal(t, 40.0, rect.CenterY())
}
