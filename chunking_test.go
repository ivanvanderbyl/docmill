package docmill_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	docmill "github.com/ivanvanderbyl/docmill"
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
	doc := &docmill.Document{
		Pages: []docmill.Page{
			{
				Number: 1,
				Paragraphs: []docmill.Paragraph{
					{
						IsHeading:    true,
						HeadingLevel: 1,
						Lines: []docmill.Line{
							{Words: []docmill.EnrichedWord{{Text: "Introduction"}}},
						},
					},
					{
						Lines: []docmill.Line{
							{Words: []docmill.EnrichedWord{{Text: "This"}, {Text: "is"}, {Text: "a"}, {Text: "test"}, {Text: "paragraph."}}},
						},
					},
				},
			},
		},
	}

	cc := docmill.ChunkConfig{MaxTokens: 2048}
	chunks := doc.ToChunks(docmill.DefaultConfig(), cc)

	require.NotEmpty(t, chunks)
	assert.Equal(t, 0, chunks[0].Index)
	assert.Equal(t, 1, chunks[0].StartPage)
	assert.Equal(t, 1, chunks[0].EndPage)
	assert.Contains(t, chunks[0].Text, "Introduction")
	assert.Contains(t, chunks[0].Text, "test paragraph")
}

func TestToChunks_SplitsOnMaxTokens(t *testing.T) {
	var paragraphs []docmill.Paragraph
	paragraphs = append(paragraphs, docmill.Paragraph{
		IsHeading:    true,
		HeadingLevel: 1,
		Lines: []docmill.Line{
			{Words: []docmill.EnrichedWord{{Text: "Title"}}},
		},
	})

	for i := 0; i < 20; i++ {
		paragraphs = append(paragraphs, docmill.Paragraph{
			Lines: []docmill.Line{
				{Words: []docmill.EnrichedWord{
					{Text: "Lorem"}, {Text: "ipsum"}, {Text: "dolor"}, {Text: "sit"}, {Text: "amet,"},
					{Text: "consectetur"}, {Text: "adipiscing"}, {Text: "elit."}, {Text: "Sed"}, {Text: "do"},
					{Text: "eiusmod"}, {Text: "tempor"}, {Text: "incididunt"}, {Text: "ut"}, {Text: "labore."},
				}},
			},
		})
	}

	doc := &docmill.Document{
		Pages: []docmill.Page{
			{Number: 1, Paragraphs: paragraphs},
		},
	}

	cc := docmill.ChunkConfig{MaxTokens: 64}
	chunks := doc.ToChunks(docmill.DefaultConfig(), cc)

	require.Greater(t, len(chunks), 1, "should produce multiple chunks with small MaxTokens")
	for _, chunk := range chunks {
		assert.NotEmpty(t, chunk.Text)
		assert.Equal(t, 1, chunk.StartPage)
	}
}

func TestToChunks_HeadingPathTracking(t *testing.T) {
	doc := &docmill.Document{
		Pages: []docmill.Page{
			{
				Number: 1,
				Paragraphs: []docmill.Paragraph{
					{
						IsHeading: true, HeadingLevel: 1,
						Lines: []docmill.Line{{Words: []docmill.EnrichedWord{{Text: "Chapter"}, {Text: "One"}}}},
					},
					{
						IsHeading: true, HeadingLevel: 2,
						Lines: []docmill.Line{{Words: []docmill.EnrichedWord{{Text: "Section"}, {Text: "A"}}}},
					},
					{
						Lines: []docmill.Line{{Words: []docmill.EnrichedWord{{Text: "Content"}, {Text: "here."}}}},
					},
				},
			},
		},
	}

	cc := docmill.ChunkConfig{MaxTokens: 2048}
	chunks := doc.ToChunks(docmill.DefaultConfig(), cc)

	require.Len(t, chunks, 1)
	require.Len(t, chunks[0].HeadingPath, 1)
	assert.Equal(t, "Chapter One", chunks[0].HeadingPath[0].Text)
}

func TestToChunks_RepeatHeadings(t *testing.T) {
	var paragraphs []docmill.Paragraph
	paragraphs = append(paragraphs, docmill.Paragraph{
		IsHeading: true, HeadingLevel: 1,
		Lines: []docmill.Line{{Words: []docmill.EnrichedWord{{Text: "Main"}, {Text: "Title"}}}},
	})
	for i := 0; i < 30; i++ {
		paragraphs = append(paragraphs, docmill.Paragraph{
			Lines: []docmill.Line{
				{Words: []docmill.EnrichedWord{
					{Text: "Some"}, {Text: "content"}, {Text: "paragraph"}, {Text: "number"},
					{Text: "with"}, {Text: "enough"}, {Text: "words"}, {Text: "to"}, {Text: "fill."},
				}},
			},
		})
	}

	doc := &docmill.Document{
		Pages: []docmill.Page{
			{Number: 1, Paragraphs: paragraphs},
		},
	}

	cc := docmill.ChunkConfig{
		MaxTokens:      64,
		RepeatHeadings: true,
	}
	chunks := doc.ToChunks(docmill.DefaultConfig(), cc)

	require.Greater(t, len(chunks), 1)
	for i, chunk := range chunks {
		if i > 0 {
			assert.Contains(t, chunk.Text, "Main Title", "later chunks should repeat heading")
		}
	}
}

func TestToChunks_Overlap(t *testing.T) {
	var paragraphs []docmill.Paragraph
	for i := 0; i < 10; i++ {
		paragraphs = append(paragraphs, docmill.Paragraph{
			Lines: []docmill.Line{
				{Words: []docmill.EnrichedWord{
					{Text: "Paragraph"}, {Text: "number"}, {Text: "with"}, {Text: "several"},
					{Text: "words"}, {Text: "to"}, {Text: "take"}, {Text: "up"}, {Text: "space."},
				}},
			},
		})
	}

	doc := &docmill.Document{
		Pages: []docmill.Page{
			{Number: 1, Paragraphs: paragraphs},
		},
	}

	cc := docmill.ChunkConfig{
		MaxTokens:     32,
		OverlapTokens: 16,
	}
	chunks := doc.ToChunks(docmill.DefaultConfig(), cc)

	require.Greater(t, len(chunks), 1, "should produce multiple chunks")
}

func TestToChunks_MultiPage(t *testing.T) {
	doc := &docmill.Document{
		Pages: []docmill.Page{
			{
				Number: 1,
				Paragraphs: []docmill.Paragraph{
					{Lines: []docmill.Line{{Words: []docmill.EnrichedWord{{Text: "Page"}, {Text: "one"}, {Text: "content."}}}}},
				},
			},
			{
				Number: 2,
				Paragraphs: []docmill.Paragraph{
					{Lines: []docmill.Line{{Words: []docmill.EnrichedWord{{Text: "Page"}, {Text: "two"}, {Text: "content."}}}}},
				},
			},
		},
	}

	cc := docmill.ChunkConfig{MaxTokens: 2048}
	config := docmill.DefaultConfig()
	config.IncludePageBreaks = false
	chunks := doc.ToChunks(config, cc)

	require.Len(t, chunks, 1)
	assert.Equal(t, 1, chunks[0].StartPage)
	assert.Equal(t, 2, chunks[0].EndPage)
}

func TestToChunks_EmptyDocument(t *testing.T) {
	doc := &docmill.Document{}
	cc := docmill.DefaultChunkConfig()
	chunks := doc.ToChunks(docmill.DefaultConfig(), cc)
	assert.Empty(t, chunks)
}

func TestToChunks_CustomTokenEstimator(t *testing.T) {
	doc := &docmill.Document{
		Pages: []docmill.Page{
			{
				Number: 1,
				Paragraphs: []docmill.Paragraph{
					{Lines: []docmill.Line{{Words: []docmill.EnrichedWord{{Text: "Hello"}, {Text: "world."}}}}},
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

	cc := docmill.ChunkConfig{
		MaxTokens:      100,
		EstimateTokens: wordCount,
	}
	chunks := doc.ToChunks(docmill.DefaultConfig(), cc)
	require.Len(t, chunks, 1)
	assert.Greater(t, chunks[0].TokenCount, 0)
}

func TestDefaultEstimateTokens(t *testing.T) {
	cc := docmill.DefaultChunkConfig()
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

	converter := docmill.NewConverter(instance)
	samplePath := filepath.Join("testdata", "issue-848.pdf")
	if _, err := os.Stat(samplePath); os.IsNotExist(err) {
		t.Skip("test PDF not found")
	}

	cc := docmill.ChunkConfig{MaxTokens: 256}
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

	converter := docmill.NewConverter(instance)
	samplePath := filepath.Join("testdata", "issue-848.pdf")
	if _, err := os.Stat(samplePath); os.IsNotExist(err) {
		t.Skip("test PDF not found")
	}

	md, err := converter.ConvertFile(samplePath)
	require.NoError(t, err)

	cc := docmill.ChunkConfig{MaxTokens: 100_000}
	chunks, err := converter.ConvertFileChunks(samplePath, cc)
	require.NoError(t, err)

	require.Len(t, chunks, 1, "with very large MaxTokens should produce a single chunk")
	assert.NotEmpty(t, md)
	assert.NotEmpty(t, chunks[0].Text)
}
