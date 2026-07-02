package render_test

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/render"
	"github.com/ivanvanderbyl/docmill/v2/pkg/table"
	"github.com/stretchr/testify/require"
)

func TestRenderTableDoesNotRepairMissingLigatureWordsByLiteralText(t *testing.T) {
	t.Parallel()

	data := table.Data{
		NumRows: 2,
		NumCols: 1,
		Cells: []table.Cell{
			{Text: "Observation", RowSpan: 1, ColSpan: 1, StartRow: 0, EndRow: 1, StartCol: 0, EndCol: 1},
			{Text: "signi cant modi cations", RowSpan: 1, ColSpan: 1, StartRow: 1, EndRow: 2, StartCol: 0, EndCol: 1},
		},
	}

	got, err := render.Table(data)

	require.NoError(t, err)
	require.Contains(t, got, "signi cant modi cations")
	require.NotContains(t, got, "significant modifications")
}
