package table_test

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/pkg/page"
	"github.com/ivanvanderbyl/docmill/pkg/table"
	"github.com/stretchr/testify/require"
)

func TestDetectTextTablesDoesNotPromoteBlocklistPatternsByLiteralText(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		textCell(1, "The blocklist contains the following patterns:", 0, 0, 260, 10),
		textCell(2, "# comment", 0, 20, 80, 30),
		textCell(3, "\"example.com\",", 0, 40, 120, 50),
		textCell(4, "\"example.org\",", 0, 60, 120, 70),
	}

	result := table.DetectTextTables(cells, table.DetectionOptions{})

	require.Empty(t, result.Tables)
	require.Equal(t, cells, result.TextCells)
}
