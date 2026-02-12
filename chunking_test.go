package pdfmarkdown_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pdfmarkdown "github.com/ivanvanderbyl/pdfmarkdown"
)

func newTestInstance(t *testing.T) interface{ Close() error } {
	t.Helper()
	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:  1,
		MaxIdle:  1,
		MaxTotal: 1,
	})
	require.NoError(t, err)
	t.Cleanup(func() { pool.Close() })

	instance, err := pool.GetInstance(time.Second * 30)
	require.NoError(t, err)
	return instance
}

func TestToChunks_BasicStructure(t *testing.T) {
	doc := &pdfmarkdown.Document{
		Pages: []pdfmarkdown.Page{
			{
				Number: 1,
				Paragraphs: []pdfmarkdown.Paragraph{
					{
						IsHeading:    true,
						HeadingLevel: 1,
						Lines: []pdfmarkdown.Line{
							{Words: []pdfmarkdown.EnrichedWord{{Text: "Introduction"}}},
						},
					},
					{
						Lines: []pdfmarkdown.Line{
							{Words: []pdfmarkdown.EnrichedWord{{Text: "This"}, {Text: "is"}, {Text: "a"}, {Text: "test"}, {Text: "paragraph."}}},
						},
					},
				},
			},
		},
	}

	cc := pdfmarkdown.ChunkConfig{MaxTokens: 2048}
	chunks := doc.ToChunks(pdfmarkdown.DefaultConfig(), cc)

	require.NotEmpty(t, chunks)
	assert.Equal(t, 0, chunks[0].Index)
	assert.Equal(t, 1, chunks[0].StartPage)
	assert.Equal(t, 1, chunks[0].EndPage)
	assert.Contains(t, chunks[0].Text, "Introduction")
	assert.Contains(t, chunks[0].Text, "test paragraph")
}

func TestToChunks_SplitsOnMaxTokens(t *testing.T) {
	var paragraphs []pdfmarkdown.Paragraph
	paragraphs = append(paragraphs, pdfmarkdown.Paragraph{
		IsHeading:    true,
		HeadingLevel: 1,
		Lines: []pdfmarkdown.Line{
			{Words: []pdfmarkdown.EnrichedWord{{Text: "Title"}}},
		},
	})

	for i := 0; i < 20; i++ {
		paragraphs = append(paragraphs, pdfmarkdown.Paragraph{
			Lines: []pdfmarkdown.Line{
				{Words: []pdfmarkdown.EnrichedWord{
					{Text: "Lorem"}, {Text: "ipsum"}, {Text: "dolor"}, {Text: "sit"}, {Text: "amet,"},
					{Text: "consectetur"}, {Text: "adipiscing"}, {Text: "elit."}, {Text: "Sed"}, {Text: "do"},
					{Text: "eiusmod"}, {Text: "tempor"}, {Text: "incididunt"}, {Text: "ut"}, {Text: "labore."},
				}},
			},
		})
	}

	doc := &pdfmarkdown.Document{
		Pages: []pdfmarkdown.Page{
			{Number: 1, Paragraphs: paragraphs},
		},
	}

	cc := pdfmarkdown.ChunkConfig{MaxTokens: 64}
	chunks := doc.ToChunks(pdfmarkdown.DefaultConfig(), cc)

	require.Greater(t, len(chunks), 1, "should produce multiple chunks with small MaxTokens")
	for _, chunk := range chunks {
		assert.NotEmpty(t, chunk.Text)
		assert.Equal(t, 1, chunk.StartPage)
	}
}

func TestToChunks_HeadingPathTracking(t *testing.T) {
	doc := &pdfmarkdown.Document{
		Pages: []pdfmarkdown.Page{
			{
				Number: 1,
				Paragraphs: []pdfmarkdown.Paragraph{
					{
						IsHeading: true, HeadingLevel: 1,
						Lines: []pdfmarkdown.Line{{Words: []pdfmarkdown.EnrichedWord{{Text: "Chapter"}, {Text: "One"}}}},
					},
					{
						IsHeading: true, HeadingLevel: 2,
						Lines: []pdfmarkdown.Line{{Words: []pdfmarkdown.EnrichedWord{{Text: "Section"}, {Text: "A"}}}},
					},
					{
						Lines: []pdfmarkdown.Line{{Words: []pdfmarkdown.EnrichedWord{{Text: "Content"}, {Text: "here."}}}},
					},
				},
			},
		},
	}

	cc := pdfmarkdown.ChunkConfig{MaxTokens: 2048}
	chunks := doc.ToChunks(pdfmarkdown.DefaultConfig(), cc)

	require.Len(t, chunks, 1)
	require.Len(t, chunks[0].HeadingPath, 1)
	assert.Equal(t, "Chapter One", chunks[0].HeadingPath[0].Text)
}

func TestToChunks_RepeatHeadings(t *testing.T) {
	var paragraphs []pdfmarkdown.Paragraph
	paragraphs = append(paragraphs, pdfmarkdown.Paragraph{
		IsHeading: true, HeadingLevel: 1,
		Lines: []pdfmarkdown.Line{{Words: []pdfmarkdown.EnrichedWord{{Text: "Main"}, {Text: "Title"}}}},
	})
	for i := 0; i < 30; i++ {
		paragraphs = append(paragraphs, pdfmarkdown.Paragraph{
			Lines: []pdfmarkdown.Line{
				{Words: []pdfmarkdown.EnrichedWord{
					{Text: "Some"}, {Text: "content"}, {Text: "paragraph"}, {Text: "number"},
					{Text: "with"}, {Text: "enough"}, {Text: "words"}, {Text: "to"}, {Text: "fill."},
				}},
			},
		})
	}

	doc := &pdfmarkdown.Document{
		Pages: []pdfmarkdown.Page{
			{Number: 1, Paragraphs: paragraphs},
		},
	}

	cc := pdfmarkdown.ChunkConfig{
		MaxTokens:      64,
		RepeatHeadings: true,
	}
	chunks := doc.ToChunks(pdfmarkdown.DefaultConfig(), cc)

	require.Greater(t, len(chunks), 1)
	for i, chunk := range chunks {
		if i > 0 {
			assert.Contains(t, chunk.Text, "Main Title", "later chunks should repeat heading")
		}
	}
}

func TestToChunks_Overlap(t *testing.T) {
	var paragraphs []pdfmarkdown.Paragraph
	for i := 0; i < 10; i++ {
		paragraphs = append(paragraphs, pdfmarkdown.Paragraph{
			Lines: []pdfmarkdown.Line{
				{Words: []pdfmarkdown.EnrichedWord{
					{Text: "Paragraph"}, {Text: "number"}, {Text: "with"}, {Text: "several"},
					{Text: "words"}, {Text: "to"}, {Text: "take"}, {Text: "up"}, {Text: "space."},
				}},
			},
		})
	}

	doc := &pdfmarkdown.Document{
		Pages: []pdfmarkdown.Page{
			{Number: 1, Paragraphs: paragraphs},
		},
	}

	cc := pdfmarkdown.ChunkConfig{
		MaxTokens:     32,
		OverlapTokens: 16,
	}
	chunks := doc.ToChunks(pdfmarkdown.DefaultConfig(), cc)

	require.Greater(t, len(chunks), 1, "should produce multiple chunks")
}

func TestToChunks_MultiPage(t *testing.T) {
	doc := &pdfmarkdown.Document{
		Pages: []pdfmarkdown.Page{
			{
				Number: 1,
				Paragraphs: []pdfmarkdown.Paragraph{
					{Lines: []pdfmarkdown.Line{{Words: []pdfmarkdown.EnrichedWord{{Text: "Page"}, {Text: "one"}, {Text: "content."}}}}},
				},
			},
			{
				Number: 2,
				Paragraphs: []pdfmarkdown.Paragraph{
					{Lines: []pdfmarkdown.Line{{Words: []pdfmarkdown.EnrichedWord{{Text: "Page"}, {Text: "two"}, {Text: "content."}}}}},
				},
			},
		},
	}

	cc := pdfmarkdown.ChunkConfig{MaxTokens: 2048}
	config := pdfmarkdown.DefaultConfig()
	config.IncludePageBreaks = false
	chunks := doc.ToChunks(config, cc)

	require.Len(t, chunks, 1)
	assert.Equal(t, 1, chunks[0].StartPage)
	assert.Equal(t, 2, chunks[0].EndPage)
}

func TestToChunks_EmptyDocument(t *testing.T) {
	doc := &pdfmarkdown.Document{}
	cc := pdfmarkdown.DefaultChunkConfig()
	chunks := doc.ToChunks(pdfmarkdown.DefaultConfig(), cc)
	assert.Empty(t, chunks)
}

func TestToChunks_CustomTokenEstimator(t *testing.T) {
	doc := &pdfmarkdown.Document{
		Pages: []pdfmarkdown.Page{
			{
				Number: 1,
				Paragraphs: []pdfmarkdown.Paragraph{
					{Lines: []pdfmarkdown.Line{{Words: []pdfmarkdown.EnrichedWord{{Text: "Hello"}, {Text: "world."}}}}},
				},
			},
		},
	}

	wordCount := func(s string) int {
		count := 0
		for _, c := range s {
			if c == ' ' || c == '\n' {
				count++
			}
		}
		return count + 1
	}

	cc := pdfmarkdown.ChunkConfig{
		MaxTokens:      100,
		EstimateTokens: wordCount,
	}
	chunks := doc.ToChunks(pdfmarkdown.DefaultConfig(), cc)
	require.Len(t, chunks, 1)
	assert.Greater(t, chunks[0].TokenCount, 0)
}

func TestDefaultEstimateTokens(t *testing.T) {
	cc := pdfmarkdown.DefaultChunkConfig()
	assert.Nil(t, cc.EstimateTokens)
	assert.Equal(t, 512, cc.MaxTokens)
}

func TestConvertFileChunks(t *testing.T) {
	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:  1,
		MaxIdle:  1,
		MaxTotal: 1,
	})
	require.NoError(t, err)
	defer pool.Close()

	instance, err := pool.GetInstance(time.Second * 30)
	require.NoError(t, err)

	converter := pdfmarkdown.NewConverter(instance)
	samplePath := filepath.Join("testdata", "issue-848.pdf")
	if _, err := os.Stat(samplePath); os.IsNotExist(err) {
		t.Skip("test PDF not found")
	}

	cc := pdfmarkdown.ChunkConfig{MaxTokens: 256}
	chunks, err := converter.ConvertFileChunks(samplePath, cc)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)

	for i, chunk := range chunks {
		assert.Equal(t, i, chunk.Index)
		assert.NotEmpty(t, chunk.Text)
		assert.Greater(t, chunk.TokenCount, 0)
		assert.Greater(t, chunk.StartPage, 0)
		assert.GreaterOrEqual(t, chunk.EndPage, chunk.StartPage)
	}
}

func TestConvertFileChunks_ConsistentWithToMarkdown(t *testing.T) {
	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:  1,
		MaxIdle:  1,
		MaxTotal: 1,
	})
	require.NoError(t, err)
	defer pool.Close()

	instance, err := pool.GetInstance(time.Second * 30)
	require.NoError(t, err)

	converter := pdfmarkdown.NewConverter(instance)
	samplePath := filepath.Join("testdata", "issue-848.pdf")
	if _, err := os.Stat(samplePath); os.IsNotExist(err) {
		t.Skip("test PDF not found")
	}

	md, err := converter.ConvertFile(samplePath)
	require.NoError(t, err)

	cc := pdfmarkdown.ChunkConfig{MaxTokens: 100_000}
	chunks, err := converter.ConvertFileChunks(samplePath, cc)
	require.NoError(t, err)

	require.Len(t, chunks, 1, "with very large MaxTokens should produce a single chunk")
	assert.NotEmpty(t, md)
	assert.NotEmpty(t, chunks[0].Text)
}
