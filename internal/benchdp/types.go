package benchdp

// Scores mirrors the accuracy section of the cross-tool benchmark.
// Values are normalised to [0, 1], where higher is better.
type Scores struct {
	ExtractionAccuracy float64 `json:"extraction_accuracy"`
	ReadingOrderNID    float64 `json:"reading_order_nid"`
	TableStructureTEDS float64 `json:"table_structure_teds"`
	HeadingLevelMHS    float64 `json:"heading_level_mhs"`
}

// DocumentCase is one PDF plus its ground-truth Markdown reference.
type DocumentCase struct {
	ID              string `json:"id"`
	PDFPath         string `json:"pdf_path"`
	GroundTruthPath string `json:"ground_truth_path"`
	Pages           int    `json:"pages,omitempty"`
}
