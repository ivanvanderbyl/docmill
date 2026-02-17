package pdfmarkdown

import (
	"io"
	"log"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/pkg/errors"
	"github.com/sourcegraph/conc/stream"
)

// ProcessingMetrics contains timing and statistics for PDF conversion
type ProcessingMetrics struct {
	TotalTime       time.Duration
	DocumentOpen    time.Duration
	PageExtractions []PageMetrics
	Statistics      DocumentStatistics
}

// PageMetrics contains timing for a single page
type PageMetrics struct {
	PageNumber int
	Duration   time.Duration
}

// DocumentStatistics contains document-level statistics
type DocumentStatistics struct {
	TotalPages      int
	TotalParagraphs int
	TotalTables     int
	TotalHeadings   int
	TotalWords      int
	TotalCharacters int
}

// Config controls markdown conversion behavior.
type Config struct {
	// IncludePageBreaks adds "---" separators between pages (default: true)
	IncludePageBreaks bool

	// MinHeadingFontSize is the minimum font size difference to detect headings
	// A value of 0 disables size-based heading detection (default: 1.15x body text)
	MinHeadingFontSize float64

	// DetectTables enables table detection and extraction (default: false)
	DetectTables bool

	// TableSettings configures table detection behavior (default: DefaultTableSettings())
	TableSettings TableSettings

	// UseSegmentBasedTables enables PDF-TREX segment-based table detection
	// This works better for tables without ruling lines (default: true)
	UseSegmentBasedTables bool

	// UseAdaptiveThresholds enables document-specific threshold calculation
	// Based on spacing distribution analysis (default: true)
	UseAdaptiveThresholds bool

	// EnableMetricsLogging enables processing time and statistics logging (default: false)
	EnableMetricsLogging bool

	// MaxConcurrency controls how many pages are processed concurrently during
	// the structure detection phase. PDFium extraction is always sequential,
	// but paragraph/table/heading detection runs in parallel. (default: 10)
	MaxConcurrency int
}

// DefaultConfig returns the default converter configuration.
func DefaultConfig() Config {
	return Config{
		IncludePageBreaks:     true,
		MinHeadingFontSize:    1.15,
		DetectTables:          true,
		TableSettings:         DefaultTableSettings(),
		UseSegmentBasedTables: false,
		UseAdaptiveThresholds: true,
		MaxConcurrency:        10,
	}
}

// Converter converts PDFs to markdown using pdfium text extraction.
type Converter struct {
	instance pdfium.Pdfium
	config   Config
	pool     pdfium.Pool // non-nil when the Converter owns the pool
}

// New creates a new PDF to markdown converter with default configuration.
// The returned Converter manages its own pdfium pool and must be closed
// with Close when no longer needed.
func New() (*Converter, error) {
	return NewWithConfig(DefaultConfig())
}

// NewWithConfig creates a new PDF to markdown converter with custom configuration.
// The returned Converter manages its own pdfium pool and must be closed
// with Close when no longer needed.
func NewWithConfig(config Config) (*Converter, error) {
	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:  1,
		MaxIdle:  1,
		MaxTotal: 1,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to initialise pdfium")
	}

	instance, err := pool.GetInstance(time.Second * 30)
	if err != nil {
		pool.Close()
		return nil, errors.Wrap(err, "failed to get pdfium instance")
	}

	return &Converter{
		instance: instance,
		config:   config,
		pool:     pool,
	}, nil
}

// Close releases resources held by the Converter. Only required for
// converters created with New or NewWithConfig.
func (c *Converter) Close() {
	if c.pool != nil {
		c.pool.Close()
	}
}

// NewConverter creates a new PDF to markdown converter with default configuration.
// The caller is responsible for managing the pdfium pool lifecycle.
func NewConverter(instance pdfium.Pdfium) *Converter {
	return &Converter{
		instance: instance,
		config:   DefaultConfig(),
	}
}

// NewConverterWithConfig creates a new PDF to markdown converter with custom configuration.
// The caller is responsible for managing the pdfium pool lifecycle.
func NewConverterWithConfig(instance pdfium.Pdfium, config Config) *Converter {
	return &Converter{
		instance: instance,
		config:   config,
	}
}

// ConvertFile converts a PDF file to markdown.
func (c *Converter) ConvertFile(filePath string) (string, error) {
	// Open the PDF document
	doc, err := c.instance.OpenDocument(&requests.OpenDocument{
		FilePath: &filePath,
	})
	if err != nil {
		return "", errors.Wrap(err, "failed to open PDF document")
	}
	defer c.instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{
		Document: doc.Document,
	})

	return c.convertDocument(doc.Document)
}

// ConvertBytes converts PDF bytes to markdown.
func (c *Converter) ConvertBytes(pdfBytes []byte) (string, error) {
	// Open the PDF document
	doc, err := c.instance.OpenDocument(&requests.OpenDocument{
		File: &pdfBytes,
	})
	if err != nil {
		return "", errors.Wrap(err, "failed to open PDF document")
	}
	defer c.instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{
		Document: doc.Document,
	})

	return c.convertDocument(doc.Document)
}

// ConvertReader converts a PDF from an io.ReadSeeker to markdown.
func (c *Converter) ConvertReader(reader io.ReadSeeker) (string, error) {
	// Open the PDF document
	doc, err := c.instance.OpenDocument(&requests.OpenDocument{
		FileReader: reader,
	})
	if err != nil {
		return "", errors.Wrap(err, "failed to open PDF document")
	}
	defer c.instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{
		Document: doc.Document,
	})

	return c.convertDocument(doc.Document)
}

// ConvertPageRange converts a specific range of pages to markdown.
func (c *Converter) ConvertPageRange(filePath string, startPage, endPage int) (string, error) {
	// Open the PDF document
	doc, err := c.instance.OpenDocument(&requests.OpenDocument{
		FilePath: &filePath,
	})
	if err != nil {
		return "", errors.Wrap(err, "failed to open PDF document")
	}
	defer c.instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{
		Document: doc.Document,
	})

	// Get page count
	pageCount, err := c.instance.FPDF_GetPageCount(&requests.FPDF_GetPageCount{
		Document: doc.Document,
	})
	if err != nil {
		return "", errors.Wrap(err, "failed to get page count")
	}

	// Validate range
	if startPage < 0 {
		startPage = 0
	}
	if endPage < 0 || endPage >= pageCount.PageCount {
		endPage = pageCount.PageCount - 1
	}
	if startPage > endPage {
		return "", errors.New("invalid page range: start page must be <= end page")
	}

	document, _, err := c.extractAndProcessPages(doc.Document, startPage, endPage)
	if err != nil {
		return "", err
	}

	return document.ToMarkdown(c.config), nil
}

// convertDocument converts a complete PDF document to markdown.
func (c *Converter) convertDocument(docRef references.FPDF_DOCUMENT) (string, error) {
	startTime := time.Now()

	pageCount, err := c.instance.FPDF_GetPageCount(&requests.FPDF_GetPageCount{
		Document: docRef,
	})
	if err != nil {
		return "", errors.Wrap(err, "failed to get page count")
	}

	document, pageMetrics, err := c.extractAndProcessPages(docRef, 0, pageCount.PageCount-1)
	if err != nil {
		return "", err
	}

	totalTime := time.Since(startTime)

	if c.config.EnableMetricsLogging {
		for i, pm := range pageMetrics {
			log.Printf("Page %d/%d processed in %v", pm.PageNumber, pageCount.PageCount, pm.Duration)
			_ = i
		}
		logProcessingMetrics(ProcessingMetrics{
			TotalTime:       totalTime,
			PageExtractions: pageMetrics,
			Statistics:      calculateDocumentStatistics(document),
		})
	}

	return document.ToMarkdown(c.config), nil
}

// extractRawPage loads a PDF page and extracts raw data (chars, words, lines)
// using pdfium. This must be called sequentially.
func (c *Converter) extractRawPage(docRef references.FPDF_DOCUMENT, pageIndex int) (*rawPageData, error) {
	pageResp, err := c.instance.FPDF_LoadPage(&requests.FPDF_LoadPage{
		Document: docRef,
		Index:    pageIndex,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to load page")
	}
	defer c.instance.FPDF_ClosePage(&requests.FPDF_ClosePage{
		Page: pageResp.Page,
	})

	return extractRawPageData(c.instance, pageResp.Page, pageIndex+1)
}

// extractPage extracts a single page with all its structure.
func (c *Converter) extractPage(docRef references.FPDF_DOCUMENT, pageIndex int) (*Page, error) {
	raw, err := c.extractRawPage(docRef, pageIndex)
	if err != nil {
		return nil, errors.Wrap(err, "failed to extract page content")
	}
	return buildPageStructure(raw, c.config), nil
}

// extractAndProcessPages extracts pages [startPage, endPage] from the document
// and concurrently builds their structure. It uses a pipeline approach: each
// page is submitted for concurrent processing as soon as it is extracted,
// rather than buffering all raw pages in memory first.
func (c *Converter) extractAndProcessPages(docRef references.FPDF_DOCUMENT, startPage, endPage int) (*Document, []PageMetrics, error) {
	numPages := endPage - startPage + 1

	maxConc := c.config.MaxConcurrency
	if maxConc < 1 {
		maxConc = 10
	}

	document := &Document{
		Pages: make([]Page, numPages),
	}
	pageMetrics := make([]PageMetrics, numPages)

	s := stream.New().WithMaxGoroutines(maxConc)
	for i := startPage; i <= endPage; i++ {
		raw, err := c.extractRawPage(docRef, i)
		if err != nil {
			s.Wait()
			return nil, nil, errors.Wrapf(err, "failed to extract page %d", i+1)
		}

		idx := i - startPage
		s.Go(func() stream.Callback {
			pageStart := time.Now()
			page := buildPageStructure(raw, c.config)
			dur := time.Since(pageStart)

			return func() {
				document.Pages[idx] = *page
				pageMetrics[idx] = PageMetrics{
					PageNumber: i + 1,
					Duration:   dur,
				}
			}
		})
	}
	s.Wait()

	return document, pageMetrics, nil
}

// calculateDocumentStatistics calculates statistics for the document
func calculateDocumentStatistics(doc *Document) DocumentStatistics {
	stats := DocumentStatistics{
		TotalPages: len(doc.Pages),
	}

	for _, page := range doc.Pages {
		stats.TotalParagraphs += len(page.Paragraphs)
		stats.TotalTables += len(page.Tables)

		for _, para := range page.Paragraphs {
			if para.IsHeading {
				stats.TotalHeadings++
			}

			for _, line := range para.Lines {
				stats.TotalWords += len(line.Words)
				for _, word := range line.Words {
					stats.TotalCharacters += len(word.Text)
				}
			}
		}
	}

	return stats
}

// logProcessingMetrics logs the processing metrics in a readable format
func logProcessingMetrics(metrics ProcessingMetrics) {
	log.Println("┌─────────────────────────────────────────────┐")
	log.Println("│ PDF Processing Metrics                      │")
	log.Println("├─────────────────────────────────────────────┤")
	log.Printf("│ Total Time: %-31v │\n", metrics.TotalTime.Round(time.Millisecond))
	log.Println("├─────────────────────────────────────────────┤")
	log.Println("│ Document Statistics                         │")
	log.Println("├─────────────────────────────────────────────┤")
	log.Printf("│   Pages:      %-29d │\n", metrics.Statistics.TotalPages)
	log.Printf("│   Paragraphs: %-29d │\n", metrics.Statistics.TotalParagraphs)
	log.Printf("│   Headings:   %-29d │\n", metrics.Statistics.TotalHeadings)
	log.Printf("│   Tables:     %-29d │\n", metrics.Statistics.TotalTables)
	log.Printf("│   Words:      %-29d │\n", metrics.Statistics.TotalWords)
	log.Printf("│   Characters: %-29d │\n", metrics.Statistics.TotalCharacters)
	log.Println("├─────────────────────────────────────────────┤")
	log.Println("│ Per-Page Timing                             │")
	log.Println("├─────────────────────────────────────────────┤")

	// Show timing for each page
	for _, pm := range metrics.PageExtractions {
		log.Printf("│   Page %2d: %-30v │\n", pm.PageNumber, pm.Duration.Round(time.Millisecond))
	}

	// Show average time per page
	if len(metrics.PageExtractions) > 0 {
		avgTime := metrics.TotalTime / time.Duration(len(metrics.PageExtractions))
		log.Println("├─────────────────────────────────────────────┤")
		log.Printf("│ Avg per page: %-28v │\n", avgTime.Round(time.Millisecond))
	}

	log.Println("└─────────────────────────────────────────────┘")
}

// ConvertFileWithMetrics converts a PDF and returns both markdown and metrics
func (c *Converter) ConvertFileWithMetrics(filePath string) (string, ProcessingMetrics, error) {
	startTime := time.Now()
	openStart := time.Now()

	doc, err := c.instance.OpenDocument(&requests.OpenDocument{
		FilePath: &filePath,
	})
	if err != nil {
		return "", ProcessingMetrics{}, errors.Wrap(err, "failed to open PDF document")
	}
	defer c.instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{
		Document: doc.Document,
	})

	documentOpenTime := time.Since(openStart)

	pageCount, err := c.instance.FPDF_GetPageCount(&requests.FPDF_GetPageCount{
		Document: doc.Document,
	})
	if err != nil {
		return "", ProcessingMetrics{}, errors.Wrap(err, "failed to get page count")
	}

	document, pageMetrics, err := c.extractAndProcessPages(doc.Document, 0, pageCount.PageCount-1)
	if err != nil {
		return "", ProcessingMetrics{}, err
	}

	markdownOutput := document.ToMarkdown(c.config)
	totalTime := time.Since(startTime)

	metrics := ProcessingMetrics{
		TotalTime:       totalTime,
		DocumentOpen:    documentOpenTime,
		PageExtractions: pageMetrics,
		Statistics:      calculateDocumentStatistics(document),
	}

	return markdownOutput, metrics, nil
}

// GetDocumentInfo returns basic information about a PDF without converting it.
func (c *Converter) GetDocumentInfo(filePath string) (*DocumentInfo, error) {
	doc, err := c.instance.OpenDocument(&requests.OpenDocument{
		FilePath: &filePath,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to open PDF document")
	}
	defer c.instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{
		Document: doc.Document,
	})

	pageCount, err := c.instance.FPDF_GetPageCount(&requests.FPDF_GetPageCount{
		Document: doc.Document,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to get page count")
	}

	return &DocumentInfo{
		PageCount: pageCount.PageCount,
	}, nil
}

// DocumentInfo contains basic information about a PDF document.
type DocumentInfo struct {
	PageCount int
}
