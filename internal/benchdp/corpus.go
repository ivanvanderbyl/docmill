package benchdp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	"github.com/ivanvanderbyl/docmill/v2/pkg/pdf"
)

type Corpus struct {
	Name                  string         `json:"name"`
	Source                string         `json:"source,omitempty"`
	SkippedImageOnlyCases int            `json:"skipped_image_only_cases,omitempty"`
	Cases                 []DocumentCase `json:"cases"`
}

func LoadCorpus(root string) (Corpus, error) {
	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return Corpus{}, fmt.Errorf("read corpus manifest: %w", err)
	}

	var corpus Corpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		return Corpus{}, fmt.Errorf("parse corpus manifest: %w", err)
	}
	if corpus.Name == "" {
		return Corpus{}, fmt.Errorf("corpus manifest missing name")
	}

	backend := parser.NewBackend()
	defer backend.Close()

	seen := make(map[string]bool, len(corpus.Cases))
	filtered := make([]DocumentCase, 0, len(corpus.Cases))
	for i := range corpus.Cases {
		c := &corpus.Cases[i]
		if c.ID == "" {
			return Corpus{}, fmt.Errorf("corpus case %d missing id", i)
		}
		if seen[c.ID] {
			return Corpus{}, fmt.Errorf("duplicate corpus case id %q", c.ID)
		}
		seen[c.ID] = true

		c.PDFPath = resolveCorpusPath(root, c.PDFPath)
		c.GroundTruthPath = resolveCorpusPath(root, c.GroundTruthPath)
		if err := requireFile(c.PDFPath); err != nil {
			return Corpus{}, fmt.Errorf("case %q pdf: %w", c.ID, err)
		}
		if err := requireFile(c.GroundTruthPath); err != nil {
			return Corpus{}, fmt.Errorf("case %q ground truth: %w", c.ID, err)
		}

		hasText, pages, err := hasNativeText(context.Background(), backend, c.PDFPath)
		if err != nil {
			return Corpus{}, fmt.Errorf("case %q native text check: %w", c.ID, err)
		}
		if c.Pages <= 0 && pages > 0 {
			c.Pages = pages
		}
		if !hasText {
			corpus.SkippedImageOnlyCases++
			continue
		}
		filtered = append(filtered, *c)
	}
	corpus.Cases = filtered

	sort.SliceStable(corpus.Cases, func(i, j int) bool {
		return corpus.Cases[i].ID < corpus.Cases[j].ID
	})
	return corpus, nil
}

func hasNativeText(ctx context.Context, backend pdf.Backend, path string) (bool, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, 0, err
	}
	doc, err := backend.OpenBytes(ctx, data)
	if err != nil {
		return false, 0, err
	}
	defer doc.Close()

	pages, err := doc.PageCount(ctx)
	if err != nil {
		return false, 0, err
	}
	for i := range pages {
		page, err := doc.Page(ctx, i)
		if err != nil {
			return false, pages, err
		}
		cells, err := page.TextCells(ctx)
		if err != nil {
			return false, pages, err
		}
		for _, cell := range cells {
			if strings.TrimSpace(cell.Text) != "" {
				return true, pages, nil
			}
		}
	}
	return false, pages, nil
}

func resolveCorpusPath(root, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(root, path)
}

func requireFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	return nil
}
