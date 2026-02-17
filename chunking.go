package docmill

import (
	"bytes"
	"strings"

	"github.com/ivanvanderbyl/markdown"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/pkg/errors"
)

type ChunkConfig struct {
	MaxTokens      int
	OverlapTokens  int
	RepeatHeadings bool
	EstimateTokens func(s string) int
}

func DefaultChunkConfig() ChunkConfig {
	return ChunkConfig{
		MaxTokens:     512,
		OverlapTokens: 0,
	}
}

type HeadingContext struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	Page  int    `json:"page"`
}

type Chunk struct {
	Index      int    `json:"index"`
	Text       string `json:"text"`
	TokenCount int    `json:"token_count"`

	StartPage int `json:"start_page"`
	EndPage   int `json:"end_page"`

	HeadingPath []HeadingContext `json:"heading_path,omitempty"`
}

type blockKind int

const (
	blockHeading blockKind = iota
	blockParagraph
	blockTable
	blockPageBreak
)

type mdBlock struct {
	kind        blockKind
	page        int
	markdown    string
	tokens      int
	headingPath []HeadingContext
}

func defaultEstimateTokens(s string) int {
	return (len(s) + 3) / 4
}

func (cc ChunkConfig) estimateTokens(s string) int {
	if cc.EstimateTokens != nil {
		return cc.EstimateTokens(s)
	}
	return defaultEstimateTokens(s)
}

func renderParagraphMarkdown(para Paragraph) string {
	var buf bytes.Buffer
	md := markdown.NewMarkdown(&buf)
	convertParagraphToMarkdown(md, para)
	md.LF()
	_ = md.Build()
	return buf.String()
}

func renderTableMarkdown(table Table) string {
	var buf bytes.Buffer
	md := markdown.NewMarkdown(&buf)
	convertTableToMarkdown(md, table)
	md.LF()
	_ = md.Build()
	return buf.String()
}

func headingText(para Paragraph) string {
	if len(para.Lines) == 0 || len(para.Lines[0].Words) == 0 {
		return ""
	}
	words := make([]string, len(para.Lines[0].Words))
	for i, w := range para.Lines[0].Words {
		words[i] = w.Text
	}
	return strings.TrimRight(strings.Join(words, " "), " \t")
}

func copyHeadingPath(path []HeadingContext) []HeadingContext {
	if len(path) == 0 {
		return nil
	}
	cp := make([]HeadingContext, len(path))
	copy(cp, path)
	return cp
}

func buildBlocks(doc *Document, config Config, cc ChunkConfig) []mdBlock {
	var blocks []mdBlock
	var headingStack []HeadingContext

	for i, page := range doc.Pages {
		if i > 0 && config.IncludePageBreaks {
			blocks = append(blocks, mdBlock{
				kind:        blockPageBreak,
				page:        page.Number,
				headingPath: copyHeadingPath(headingStack),
			})
		}

		for _, para := range page.Paragraphs {
			if para.IsHeading {
				level := para.HeadingLevel
				truncated := headingStack[:0:0]
				for _, h := range headingStack {
					if h.Level < level {
						truncated = append(truncated, h)
					} else {
						break
					}
				}
				truncated = append(truncated, HeadingContext{
					Level: level,
					Text:  headingText(para),
					Page:  page.Number,
				})
				headingStack = truncated

				text := renderParagraphMarkdown(para)
				blocks = append(blocks, mdBlock{
					kind:        blockHeading,
					page:        page.Number,
					markdown:    text,
					tokens:      cc.estimateTokens(text),
					headingPath: copyHeadingPath(headingStack),
				})
			} else {
				text := renderParagraphMarkdown(para)
				blocks = append(blocks, mdBlock{
					kind:        blockParagraph,
					page:        page.Number,
					markdown:    text,
					tokens:      cc.estimateTokens(text),
					headingPath: copyHeadingPath(headingStack),
				})
			}
		}

		if config.DetectTables && len(page.Tables) > 0 {
			for _, table := range page.Tables {
				text := renderTableMarkdown(table)
				blocks = append(blocks, mdBlock{
					kind:        blockTable,
					page:        page.Number,
					markdown:    text,
					tokens:      cc.estimateTokens(text),
					headingPath: copyHeadingPath(headingStack),
				})
			}
		}
	}

	return blocks
}

func renderHeadingPrefix(path []HeadingContext) string {
	if len(path) == 0 {
		return ""
	}
	var buf bytes.Buffer
	md := markdown.NewMarkdown(&buf)
	for _, h := range path {
		switch h.Level {
		case 1:
			md.H1(h.Text)
		case 2:
			md.H2(h.Text)
		case 3:
			md.H3(h.Text)
		case 4:
			md.H4(h.Text)
		case 5:
			md.H5(h.Text)
		case 6:
			md.H6(h.Text)
		default:
			md.H1(h.Text)
		}
		md.LF()
	}
	_ = md.Build()
	return buf.String()
}

func packChunks(blocks []mdBlock, cc ChunkConfig) []Chunk {
	if len(blocks) == 0 {
		return nil
	}

	var chunks []Chunk
	chunkIdx := 0
	i := 0

	for i < len(blocks) {
		var chunkBlocks []int
		chunkTokens := 0

		prefix := ""
		prefixTokens := 0
		if cc.RepeatHeadings && len(blocks[i].headingPath) > 0 {
			prefix = renderHeadingPrefix(blocks[i].headingPath)
			prefixTokens = cc.estimateTokens(prefix)
		}

		for i < len(blocks) {
			if blocks[i].kind == blockPageBreak {
				i++
				continue
			}

			needed := blocks[i].tokens
			currentTotal := chunkTokens + prefixTokens
			if len(chunkBlocks) > 0 && currentTotal+needed > cc.MaxTokens {
				break
			}

			chunkBlocks = append(chunkBlocks, i)
			chunkTokens += needed
			i++
		}

		if len(chunkBlocks) == 0 {
			continue
		}

		var parts []string
		if prefix != "" {
			parts = append(parts, strings.TrimRight(prefix, "\n"))
		}
		for _, bi := range chunkBlocks {
			trimmed := strings.TrimRight(blocks[bi].markdown, "\n")
			if trimmed != "" {
				parts = append(parts, trimmed)
			}
		}
		text := strings.Join(parts, "\n")

		startPage := blocks[chunkBlocks[0]].page
		endPage := blocks[chunkBlocks[len(chunkBlocks)-1]].page

		chunks = append(chunks, Chunk{
			Index:       chunkIdx,
			Text:        text,
			TokenCount:  cc.estimateTokens(text),
			StartPage:   startPage,
			EndPage:     endPage,
			HeadingPath: copyHeadingPath(blocks[chunkBlocks[0]].headingPath),
		})
		chunkIdx++

		if cc.OverlapTokens > 0 && i < len(blocks) {
			overlapTokens := 0
			rewind := len(chunkBlocks) - 1
			for rewind >= 0 && overlapTokens < cc.OverlapTokens {
				bi := chunkBlocks[rewind]
				overlapTokens += blocks[bi].tokens
				rewind--
			}
			rewind++
			newStart := chunkBlocks[rewind]
			if newStart > chunkBlocks[0] {
				i = newStart
			}
		}
	}

	return chunks
}

func (d *Document) ToChunks(config Config, cc ChunkConfig) []Chunk {
	normalizeDocumentHeadings(d)
	blocks := buildBlocks(d, config, cc)
	return packChunks(blocks, cc)
}

func (c *Converter) extractDocument(doc references.FPDF_DOCUMENT) (*Document, error) {
	pageCount, err := c.instance.FPDF_GetPageCount(&requests.FPDF_GetPageCount{
		Document: doc,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to get page count")
	}

	document, _, err := c.extractAndProcessPages(doc, 0, pageCount.PageCount-1)
	return document, err
}

func (c *Converter) ConvertFileChunks(filePath string, cc ChunkConfig) ([]Chunk, error) {
	doc, err := c.instance.OpenDocument(&requests.OpenDocument{
		FilePath: &filePath,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to open PDF document")
	}
	defer c.instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{
		Document: doc.Document,
	})

	document, err := c.extractDocument(doc.Document)
	if err != nil {
		return nil, err
	}

	return document.ToChunks(c.config, cc), nil
}

func (c *Converter) ConvertBytesChunks(pdfBytes []byte, cc ChunkConfig) ([]Chunk, error) {
	doc, err := c.instance.OpenDocument(&requests.OpenDocument{
		File: &pdfBytes,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to open PDF document")
	}
	defer c.instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{
		Document: doc.Document,
	})

	document, err := c.extractDocument(doc.Document)
	if err != nil {
		return nil, err
	}

	return document.ToChunks(c.config, cc), nil
}


