package pdfmarkdown

import (
	"bytes"
	"sort"
	"strings"

	"github.com/ivanvanderbyl/markdown"
)

// ToMarkdown converts a document to markdown format.
func (d *Document) ToMarkdown(config Config) string {
	// Normalize heading levels across the entire document
	normalizeDocumentHeadings(d)

	var buf bytes.Buffer
	md := markdown.NewMarkdown(&buf)

	for i, page := range d.Pages {
		if i > 0 && config.IncludePageBreaks {
			md.HorizontalRule().LF()
		}

		for _, para := range page.Paragraphs {
			convertParagraphToMarkdown(md, para)
			md.LF()
		}

		// Add tables at the end of the page content
		if config.DetectTables && len(page.Tables) > 0 {
			for _, table := range page.Tables {
				convertTableToMarkdown(md, table)
				md.LF()
			}
		}
	}

	if err := md.Build(); err != nil {
		// If there's an error building the markdown, fall back to empty string
		return ""
	}

	return buf.String()
}

// normalizeDocumentHeadings adjusts heading levels across all pages to be consistent
// This ensures H1 is the largest heading across the entire document, not just within a page
func normalizeDocumentHeadings(doc *Document) {
	// Collect all heading font sizes across all pages
	type HeadingInfo struct {
		fontSize float64
		pageIdx  int
		paraIdx  int
	}

	var headings []HeadingInfo
	fontSizeSet := make(map[float64]bool)

	for pi, page := range doc.Pages {
		for pri, para := range page.Paragraphs {
			if para.IsHeading && len(para.Lines) > 0 && len(para.Lines[0].Words) > 0 {
				// Get max font size of the heading
				var maxSize float64
				for _, word := range para.Lines[0].Words {
					if word.FontSize > maxSize {
						maxSize = word.FontSize
					}
				}

				headings = append(headings, HeadingInfo{
					fontSize: maxSize,
					pageIdx:  pi,
					paraIdx:  pri,
				})
				fontSizeSet[maxSize] = true
			}
		}
	}

	if len(fontSizeSet) == 0 {
		return
	}

	// Create sorted list of unique font sizes (descending)
	var uniqueSizes []float64
	for size := range fontSizeSet {
		uniqueSizes = append(uniqueSizes, size)
	}
	sort.Float64s(uniqueSizes)
	// Reverse to descending
	for i := 0; i < len(uniqueSizes)/2; i++ {
		j := len(uniqueSizes) - 1 - i
		uniqueSizes[i], uniqueSizes[j] = uniqueSizes[j], uniqueSizes[i]
	}

	// Map font sizes to heading levels (largest = H1, etc.)
	sizeToLevel := make(map[float64]int)
	for i, size := range uniqueSizes {
		if i < 6 {
			sizeToLevel[size] = i + 1
		} else {
			sizeToLevel[size] = 6 // Max H6
		}
	}

	// Apply normalized levels to all headings
	for _, h := range headings {
		if level, ok := sizeToLevel[h.fontSize]; ok {
			doc.Pages[h.pageIdx].Paragraphs[h.paraIdx].HeadingLevel = level
		}
	}
}

// convertParagraphToMarkdown converts a single paragraph to markdown using the builder.
func convertParagraphToMarkdown(md *markdown.Markdown, para Paragraph) {
	if len(para.Lines) == 0 {
		return
	}

	// Handle headings
	if para.IsHeading {
		// For multi-line paragraphs marked as headings, only the first line is the heading
		// The rest should be rendered as regular text
		if len(para.Lines) > 1 {
			// Render first line as heading
			firstLineText := ""
			for j, word := range para.Lines[0].Words {
				if j > 0 {
					firstLineText += " "
				}
				firstLineText += word.Text
			}
			firstLineText = strings.TrimRight(firstLineText, " \t")

			switch para.HeadingLevel {
			case 1:
				md.H1(firstLineText)
			case 2:
				md.H2(firstLineText)
			case 3:
				md.H3(firstLineText)
			case 4:
				md.H4(firstLineText)
			case 5:
				md.H5(firstLineText)
			case 6:
				md.H6(firstLineText)
			default:
				md.H1(firstLineText)
			}

			// Render remaining lines as regular paragraph
			// Create a temporary non-heading paragraph for the rest
			restPara := Paragraph{
				Lines:     para.Lines[1:],
				Box:       para.Box,
				IsHeading: false,
			}
			md.LF()
			convertParagraphToMarkdown(md, restPara)
		} else {
			// Single-line heading - render normally
			text := strings.TrimRight(para.Text(), " \t")
			switch para.HeadingLevel {
			case 1:
				md.H1(text)
			case 2:
				md.H2(text)
			case 3:
				md.H3(text)
			case 4:
				md.H4(text)
			case 5:
				md.H5(text)
			case 6:
				md.H6(text)
			default:
				md.H1(text)
			}
		}
		return
	}

	// Handle code blocks
	if para.IsCode {
		text := para.Text()
		// Trim trailing whitespace from each line
		lines := strings.Split(text, "\n")
		for i, line := range lines {
			lines[i] = strings.TrimRight(line, " \t")
		}
		text = strings.Join(lines, "\n")
		md.CodeBlocks(markdown.SyntaxHighlightNone, text)
		return
	}

	// Handle lists
	if para.IsList {
		text := strings.TrimRight(para.Text(), " \t")
		// Check if it's a numbered list
		if len(text) > 0 && (text[0] >= '0' && text[0] <= '9') {
			// Extract the list item text (after the number and period)
			parts := strings.SplitN(text, ".", 2)
			if len(parts) == 2 {
				md.OrderedList(strings.TrimSpace(parts[1]))
			} else {
				md.OrderedList(text)
			}
		} else {
			// Bullet list - remove any existing bullet prefix
			text = strings.TrimPrefix(text, "* ")
			text = strings.TrimPrefix(text, "- ")
			text = strings.TrimPrefix(text, "+ ")
			md.BulletList(text)
		}
		return
	}

	// Handle regular paragraphs with inline formatting
	// Special handling: split on numbered items for better readability
	var currentSection strings.Builder
	sections := []string{}

	for _, line := range para.Lines {
		// Check if this line starts with a numbered item (2., 3., 4., etc.)
		startsWithNumber := false
		if len(line.Words) > 0 {
			firstWord := line.Words[0].Text
			if len(firstWord) >= 2 && firstWord[0] >= '2' && firstWord[0] <= '9' && firstWord[1] == '.' {
				startsWithNumber = true
			}
		}

		// If we hit a new numbered section (and we have content), save current section
		if startsWithNumber && currentSection.Len() > 0 {
			sections = append(sections, strings.TrimRight(currentSection.String(), " \t"))
			currentSection.Reset()
		}

		// Add line break before this line (unless it's the first line or start of new section)
		if currentSection.Len() > 0 {
			currentSection.WriteString("  \n")
		}

		// Build the line content using formatting runs to avoid per-word markers
		currentSection.WriteString(buildFormattedLine(line.Words))
	}

	// Add final section
	if currentSection.Len() > 0 {
		sections = append(sections, strings.TrimRight(currentSection.String(), " \t"))
	}

	// Output sections with visual separation
	if len(sections) == 1 {
		// Single section - output normally
		md.PlainText(sections[0])
	} else if len(sections) > 1 {
		// Multiple sections - add blank lines between numbered items
		for si, section := range sections {
			if section != "" {
				// Output the section
				md.PlainText(section)

				// Add visual separator after each section except the last
				if si < len(sections)-1 {
					md.LF() // End current section, creating blank line before next section
				}
			}
		}
	}
}

// wordStyle returns a comparable key representing the inline formatting of a word.
type inlineStyle struct {
	bold, italic, mono bool
}

func wordStyle(w EnrichedWord) inlineStyle {
	return inlineStyle{bold: w.IsBold, italic: w.IsItalic, mono: w.IsMonospace}
}

// buildFormattedLine groups consecutive words with the same formatting style
// into runs so that e.g. bold spans produce a single **bold run** instead of
// **word1** **word2** **word3**.
func buildFormattedLine(words []EnrichedWord) string {
	if len(words) == 0 {
		return ""
	}

	var out strings.Builder

	runStart := 0
	runStyle := wordStyle(words[0])

	flushRun := func(end int) {
		var runText strings.Builder
		for i := runStart; i < end; i++ {
			if i > runStart {
				runText.WriteString(" ")
			}
			runText.WriteString(words[i].Text)
		}

		text := runText.String()
		if runStyle.bold && runStyle.italic {
			text = markdown.BoldItalic(text)
		} else if runStyle.bold {
			text = markdown.Bold(text)
		} else if runStyle.italic {
			text = markdown.Italic(text)
		} else if runStyle.mono {
			text = markdown.Code(text)
		}

		if out.Len() > 0 {
			out.WriteString(" ")
		}
		out.WriteString(text)
	}

	for i := 1; i < len(words); i++ {
		s := wordStyle(words[i])
		if s != runStyle {
			flushRun(i)
			runStart = i
			runStyle = s
		}
	}
	flushRun(len(words))

	return out.String()
}

// applyInlineFormatting applies markdown formatting to a word based on its style.
func applyInlineFormatting(word EnrichedWord) string {
	text := word.Text

	if word.IsBold && word.IsItalic {
		return markdown.BoldItalic(text)
	}

	if word.IsBold {
		return markdown.Bold(text)
	}

	if word.IsItalic {
		return markdown.Italic(text)
	}

	if word.IsMonospace {
		return markdown.Code(text)
	}

	return text
}

// convertTableToMarkdown converts a table to markdown format using the builder.
func convertTableToMarkdown(md *markdown.Markdown, table Table) {
	if len(table.Rows) == 0 {
		return
	}

	// Convert table rows to string slices for the markdown builder
	var header []string
	var rows [][]string

	for rowIdx, row := range table.Rows {
		cells := make([]string, table.NumCols)
		for colIdx := 0; colIdx < table.NumCols; colIdx++ {
			if colIdx < len(row.Cells) {
				// Replace newlines with spaces in cell content
				cells[colIdx] = strings.ReplaceAll(row.Cells[colIdx].Content, "\n", " ")
			} else {
				cells[colIdx] = ""
			}
		}

		if rowIdx == 0 {
			// First row is the header
			header = cells
		} else {
			rows = append(rows, cells)
		}
	}

	// If we only have a header and no data rows, still create a valid table
	if len(rows) == 0 && len(header) > 0 {
		rows = [][]string{make([]string, len(header))}
	}

	md.Table(markdown.TableSet{
		Header: header,
		Rows:   rows,
	})
}

// PageToMarkdown converts a single page to markdown.
func (p *Page) ToMarkdown() string {
	var buf bytes.Buffer
	md := markdown.NewMarkdown(&buf)

	for _, para := range p.Paragraphs {
		convertParagraphToMarkdown(md, para)
		md.LF()
	}

	// Add tables at the end of the page content
	if len(p.Tables) > 0 {
		for _, table := range p.Tables {
			convertTableToMarkdown(md, table)
			md.LF()
		}
	}

	if err := md.Build(); err != nil {
		// If there's an error building the markdown, fall back to empty string
		return ""
	}

	return buf.String()
}
