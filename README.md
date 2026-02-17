# pdfmarkdown

Fast PDF to Markdown conversion using pdfium text extraction with intelligent layout and style analysis.

## Features

- **Fast extraction**: Uses native pdfium for text extraction (orders of magnitude faster than LLM processing)
- **Rich metadata**: Extracts font size, weight, style, colour, and positioning information
- **Intelligent structure detection**:
  - Headings (H1-H6) based on font size and weight
  - Paragraphs with proper line breaking and spacing
  - Bullet and numbered lists with nested items
  - Code blocks (monospace font detection)
  - Bold and italic inline formatting
  - Text alignment (left, centre, right)
  - Table detection with markdown table output
  - Multi-column layout handling with rotated text support
- **Page-aware**: Handles multi-page documents with page separators
- **Flexible API**: Convert from file path, bytes, or io.ReadSeeker
- **Configurable**: Customisable heading detection, table extraction, and formatting options
- **Performance metrics**: Optional timing and statistics logging

## Architecture

The converter works in three stages:

1. **Extraction**: Extract all characters with rich metadata (font, size, position, colour)
2. **Structure Analysis**: Group characters → words → lines → paragraphs, detect document structure
3. **Markdown Conversion**: Convert structured document to clean markdown

## Installation

```bash
go get github.com/ivanvanderbyl/pdfmarkdown
```

## Usage

### Basic Conversion

```go
import (
    "fmt"
    "log"

    "github.com/ivanvanderbyl/pdfmarkdown"
)

converter, err := pdfmarkdown.New()
if err != nil {
    log.Fatal(err)
}
defer converter.Close()

markdown, err := converter.ConvertFile("document.pdf")
if err != nil {
    log.Fatal(err)
}

fmt.Println(markdown)
```

### Custom Configuration

```go
config := pdfmarkdown.DefaultConfig()
config.IncludePageBreaks = true
config.DetectTables = true
config.UseSegmentBasedTables = true  // Better for PDFs without ruling lines
config.UseAdaptiveThresholds = true
config.MinHeadingFontSize = 1.2      // Adjust heading detection sensitivity
config.EnableMetricsLogging = true   // Enable performance metrics

converter, err := pdfmarkdown.NewWithConfig(config)
if err != nil {
    log.Fatal(err)
}
defer converter.Close()

markdown, err := converter.ConvertFile("document.pdf")
```

### Convert from Bytes

```go
pdfBytes, err := os.ReadFile("document.pdf")
if err != nil {
    log.Fatal(err)
}

markdown, err := converter.ConvertBytes(pdfBytes)
```

### Convert from io.ReadSeeker

```go
file, err := os.Open("document.pdf")
if err != nil {
    log.Fatal(err)
}
defer file.Close()

markdown, err := converter.ConvertReader(file)
```

### Convert Specific Pages

```go
// Convert pages 0-4 (first 5 pages, 0-indexed)
markdown, err := converter.ConvertPageRange("document.pdf", 0, 4)
```

### Get Document Info

```go
info, err := converter.GetDocumentInfo("document.pdf")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Document has %d pages\n", info.PageCount)
```

## Command Line Tool

A CLI tool is provided for quick conversions:

### Installation

#### Download pre-built binary

Pre-built binaries are available for Linux, macOS, and Windows (amd64 and arm64) from [GitHub Releases](https://github.com/ivanvanderbyl/pdfmarkdown/releases/latest).

**macOS / Linux:**

```bash
# Download the latest release (adjust OS and ARCH as needed)
# OS: linux, darwin  ARCH: amd64, arm64
curl -sL "https://github.com/ivanvanderbyl/pdfmarkdown/releases/latest/download/pdfmarkdown_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/').tar.gz" | tar xz -C /usr/local/bin pdfmarkdown
```

**Windows:**

Download the appropriate `.zip` from the [releases page](https://github.com/ivanvanderbyl/pdfmarkdown/releases/latest) and add `pdfmarkdown.exe` to your `PATH`.

#### Install with `go install`

```bash
go install github.com/ivanvanderbyl/pdfmarkdown/cmd/pdfmarkdown@latest
```

#### Build from source

```bash
git clone https://github.com/ivanvanderbyl/pdfmarkdown.git
cd pdfmarkdown
go install ./cmd/pdfmarkdown
```

### Usage

```bash
# Convert to file
pdfmarkdown -i input.pdf -o output.md

# Convert specific pages (0-indexed)
pdfmarkdown -i input.pdf -o output.md --start-page 0 --end-page 4

# Output to stdout
pdfmarkdown -i input.pdf

# Enable metrics logging
pdfmarkdown -i input.pdf -o output.md --metrics
```

### Options

- `-i, --input` - Input PDF file path (required)
- `-o, --output` - Output markdown file path (default: stdout)
- `--start-page` - Start page number, 0-indexed (default: all pages)
- `--end-page` - End page number, 0-indexed (default: all pages)
- `-m, --metrics` - Enable processing time and statistics logging
- `--page-breaks` - Add `---` separators between pages (default: true)
- `--min-heading-font-size` - Minimum font size multiplier to detect headings, 0 disables (default: 1.15)
- `--detect-tables` - Enable table detection and extraction (default: true)
- `--segment-tables` - Use PDF-TREX segment-based table detection, better for tables without ruling lines (default: false)
- `--adaptive-thresholds` - Enable document-specific threshold calculation based on spacing distribution (default: true)
- `--max-concurrency` - Maximum pages processed concurrently during structure detection (default: 10)
- `--chunk` - Output as JSON chunks instead of markdown
- `--chunk-max-tokens` - Maximum tokens per chunk (default: 512)
- `--chunk-overlap` - Number of overlap tokens between chunks (default: 0)
- `--chunk-repeat-headings` - Repeat heading hierarchy at the start of each chunk

## Configuration Options

### Config Struct

```go
type Config struct {
    // IncludePageBreaks adds "---" separators between pages (default: true)
    IncludePageBreaks bool

    // MinHeadingFontSize is the minimum font size multiplier to detect headings
    // A value of 0 disables size-based heading detection (default: 1.15x body text)
    MinHeadingFontSize float64

    // DetectTables enables table detection and extraction (default: true)
    DetectTables bool

    // TableSettings configures table detection behavior
    TableSettings TableSettings

    // UseSegmentBasedTables enables PDF-TREX segment-based table detection
    // This works better for tables without ruling lines (default: false)
    UseSegmentBasedTables bool

    // UseAdaptiveThresholds enables document-specific threshold calculation
    // Based on spacing distribution analysis (default: true)
    UseAdaptiveThresholds bool

    // EnableMetricsLogging enables processing time and statistics logging (default: false)
    EnableMetricsLogging bool
}
```

### Table Settings

Table detection can be configured using `TableSettings`:

```go
config := pdfmarkdown.DefaultConfig()
config.TableSettings = pdfmarkdown.TableSettings{
    VerticalStrategy:   "lines",  // "text", "lines", "lines_strict", "explicit"
    HorizontalStrategy: "lines",
    SnapTolerance:      3.0,      // Tolerance for snapping close edges
    EdgeMinLength:      3.0,      // Minimum edge length to consider
    MinWordsVertical:   3,        // Minimum words for text-based detection
    MinWordsHorizontal: 1,
}
```

## Markdown Output Features

### Headings

Headings are detected based on:
- Font size relative to body text (configurable threshold)
- Bold font weight
- Single-line paragraphs

```markdown
# Large Heading (H1)
## Medium Heading (H2)
### Smaller Heading (H3)
```

### Lists

Bullet and numbered lists with proper nesting:

```markdown
* First item
* Second item
  * Nested item
  * Another nested item

1. Numbered item
2. Another item
   1. Nested numbered item
```

### Tables

Tables are detected and converted to markdown tables:

```markdown
| Header 1 | Header 2 | Header 3 |
|----------|----------|----------|
| Cell 1   | Cell 2   | Cell 3   |
| Cell 4   | Cell 5   | Cell 6   |
```

### Inline Formatting

Bold, italic, and code are preserved:

```markdown
This is **bold** text and *italic* text with `code`.
```

### Code Blocks

Monospace paragraphs are converted to code blocks:

````markdown
```
func main() {
    fmt.Println("Hello")
}
```
````

### Page Breaks

Multi-page documents include page separators (when `IncludePageBreaks` is enabled):

```markdown
Content from page 1

---

Content from page 2
```

### Multi-Column Layouts

The converter intelligently handles multi-column layouts and rotated text, maintaining reading order where possible.

## Performance Metrics

When `EnableMetricsLogging` is enabled, the converter logs detailed timing and statistics:

```
Processing PDF with 10 pages...
Document opened in 45ms
Page 1 extracted in 23ms
Page 2 extracted in 18ms
...
Total conversion time: 234ms
Statistics:
  - Total paragraphs: 145
  - Total tables: 8
  - Total headings: 23
  - Total words: 3,456
  - Total characters: 18,234
```

Typical conversion speeds (varies by PDF complexity):
- Simple text PDF: ~10-50ms per page
- Complex formatted PDF with tables: ~50-200ms per page

Compare to LLM-based extraction:
- LLM API call: ~1-5 seconds per page
- Cost: $0 vs API costs

## Use Cases

**Ideal for pdfmarkdown:**
- PDFs with extractable text (not scanned images)
- Fast conversion without LLM API costs
- Document structure is relatively standard
- Preserving formatting (bold, italic, headings, tables)
- Batch processing large numbers of documents
- Building document search/indexing systems
- Extracting structured data from reports

**Fall back to LLM processing when:**
- PDFs are scanned images requiring OCR
- Complex semantic analysis is required
- Need to extract specific information requiring understanding
- Documents with highly irregular layouts

## Integration with LLM Pipeline

This package is designed to be a fast first pass before LLM processing:

```go
// Try fast extraction first
markdown, err := converter.ConvertFile(pdfPath)
if err != nil || len(markdown) < 100 {
    // Fall back to LLM-based extraction
    return llmExtractor.Extract(pdfPath)
}

// Use extracted markdown as LLM context for further analysis
response, err := llmClient.Analyze(ctx, llm.AnalyzeRequest{
    Context: markdown,
    Task:    "Extract key financial metrics from this report",
})
```

## Capabilities

### Supported Features

- ✅ Text extraction with font metadata
- ✅ Heading detection (H1-H6)
- ✅ Paragraph detection with proper spacing
- ✅ List detection (bullet and numbered)
- ✅ Table detection and markdown table output
- ✅ Bold and italic inline formatting
- ✅ Code block detection (monospace fonts)
- ✅ Multi-column layout handling
- ✅ Rotated text support
- ✅ Page break markers
- ✅ Configurable thresholds and settings
- ✅ Performance metrics and logging

### Current Limitations

- ❌ No OCR support (requires extractable text in PDF)
- ❌ Hyperlinks are not extracted
- ❌ Images are not extracted (text only)
- ⚠️ Complex multi-column layouts may not always preserve perfect reading order
- ⚠️ Tables without clear structure may require segment-based detection

### Experimental Features

- PDF-TREX segment-based table detection (enable with `UseSegmentBasedTables: true`)
- Adaptive threshold calculation based on document analysis

## Contributing

Contributions are welcome! Areas for improvement:
- Hyperlink extraction
- Image placeholder insertion
- Enhanced multi-column layout detection
- Custom markdown formatting options
- Additional table detection strategies

## License

MIT License - see LICENSE file for details
