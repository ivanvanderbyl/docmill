package parser_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	"github.com/stretchr/testify/require"
)

// The native replacement must avoid AI-1925's first-use shared-config failure
// and keep output serialization safe alongside other readers of one document.
func TestConcurrentPDFOperations(t *testing.T) {
	data := concurrencyPDF()
	for _, independent := range []bool{false, true} {
		t.Run(fmt.Sprintf("independent=%t", independent), func(t *testing.T) {
			shared, err := (&parser.Backend{}).OpenBytes(context.Background(), data)
			require.NoError(t, err)
			runConcurrentPDFOperations(t, data, shared.(*parser.Document), independent)
		})
	}
}

func TestNativeFirstUseIgnoresPDFCPUConfig(t *testing.T) {
	if os.Getenv("DOCMILL_FIRST_USE_CHILD") == "1" {
		runConcurrentPDFOperations(t, concurrencyPDF(), nil, true)
		return
	}

	for _, mode := range []string{"corrupt", "unavailable", "absent"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			configDir := filepath.Join(dir, "pdfcpu")
			config := filepath.Join(configDir, "config.yml")
			broken := []byte("---\nversion: [truncated\n")
			if mode == "unavailable" {
				config = filepath.Join(dir, "not-a-directory")
				dir = config
			} else if mode == "absent" {
				dir = filepath.Join(dir, "not-created")
				config = dir
			} else {
				require.NoError(t, os.Mkdir(configDir, 0700))
			}
			if mode != "absent" {
				require.NoError(t, os.WriteFile(config, broken, 0400))
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeFirstUseIgnoresPDFCPUConfig$")
			command.Env = append(os.Environ(), "DOCMILL_FIRST_USE_CHILD=1", "XDG_CONFIG_HOME="+dir)
			output, err := command.CombinedOutput()
			require.NoError(t, err, string(output))
			require.NoError(t, ctx.Err())
			if mode == "absent" {
				_, err := os.Stat(dir)
				require.ErrorIs(t, err, os.ErrNotExist)
				return
			}
			after, err := os.ReadFile(config)
			require.NoError(t, err)
			require.Equal(t, broken, after)
			if mode == "corrupt" {
				entries, err := os.ReadDir(configDir)
				require.NoError(t, err)
				require.Len(t, entries, 1)
			}
		})
	}

}

func runConcurrentPDFOperations(t *testing.T, data []byte, shared *parser.Document, independent bool) {
	t.Helper()
	ops := map[string]func(*parser.Document) error{
		"text": func(d *parser.Document) error {
			p, e := d.Page(context.Background(), 0)
			if e != nil {
				return e
			}
			_, e = p.TextCells(context.Background())
			return e
		},
		"images": func(d *parser.Document) error {
			_, e := d.ExtractImages(context.Background(), nil)
			return e
		},
		"inspection": func(d *parser.Document) error {
			_, e := d.HasActiveContent(context.Background())
			return e
		},
		"attachments": func(d *parser.Document) error {
			_, e := d.Attachments(context.Background())
			return e
		},
		"forms": func(d *parser.Document) error {
			_, e := d.AcroFormValues(context.Background())
			return e
		},
		"write": func(d *parser.Document) error {
			var out bytes.Buffer
			if err := d.WritePDF(context.Background(), &out); err != nil {
				return err
			}
			return checkConcurrentExport(out.Bytes(), 1)
		},
		"pages": func(d *parser.Document) error {
			var out bytes.Buffer
			if err := d.WritePages(context.Background(), &out, []int{0, 0}); err != nil {
				return err
			}
			return checkConcurrentExport(out.Bytes(), 2)
		},
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, len(ops)*4)
	for name, op := range ops {
		for range 4 {
			wg.Go(func() {
				<-start
				for range 8 {
					doc := shared
					if independent {
						opened, err := (&parser.Backend{}).OpenBytes(context.Background(), data)
						if err != nil {
							errs <- err
							return
						}
						doc = opened.(*parser.Document)
					}
					if err := op(doc); err != nil {
						errs <- fmt.Errorf("%s: %w", name, err)
						return
					}
				}
			})
		}
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}

func concurrencyPDF() []byte {
	return imagePDF(
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [7 0 R] >> /Names << /EmbeddedFiles << /Names [(attachment) 8 0 R] >> >> >>",
		"<< /Type /Pages /Count 1 /Kids [3 0 R] /Resources << /Font << /F1 4 0 R >> /XObject << /I 6 0 R >> >> >>",
		"<< /Parent 2 0 R /MediaBox [0 0 200 100] /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		imageStream("", "BT /F1 12 Tf 10 20 Td (parallel) Tj ET /I Do"),
		imageStream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceGray", string([]byte{128})),
		"<< /FT /Tx /T (field) /V (value) >>",
		"<< /Type /Filespec /F (attachment.txt) >>",
	)
}

func checkConcurrentExport(data []byte, wantPages int) error {
	doc, err := (&parser.Backend{}).OpenBytes(context.Background(), data)
	if err != nil {
		return err
	}
	count, err := doc.PageCount(context.Background())
	if err != nil {
		return err
	}
	if count != wantPages {
		return fmt.Errorf("exported %d pages, want %d", count, wantPages)
	}
	page, err := doc.Page(context.Background(), 0)
	if err != nil {
		return err
	}
	cells, err := page.TextCells(context.Background())
	if err != nil {
		return err
	}
	if len(cells) == 0 {
		return fmt.Errorf("exported first page lost its text")
	}
	return nil
}

func TestConcurrentEncryptedPDFOperations(t *testing.T) {
	data, err := os.ReadFile("testdata/security/r6-empty.pdf")
	require.NoError(t, err)
	for _, independent := range []bool{false, true} {
		t.Run(fmt.Sprintf("independent=%t", independent), func(t *testing.T) {
			shared, err := (&parser.Backend{}).OpenBytes(context.Background(), data)
			require.NoError(t, err)
			runConcurrentPDFOperations(t, data, shared.(*parser.Document), independent)
		})
	}
}
