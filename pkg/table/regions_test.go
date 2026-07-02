package table_test

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/table"
	"github.com/stretchr/testify/require"
)

func TestFromRegionsMatchesDoclingRegionToTableFixture(t *testing.T) {
	t.Parallel()

	tableBox := box(0, 0, 100, 175)
	rows := []geom.Box{
		box(1, 1, 99, 25),
		box(1, 25, 99, 50),
		box(1, 50, 99, 75),
		box(1, 75, 99, 99),
		box(1, 100, 99, 149),
		box(1, 150, 99, 175),
	}
	cols := []geom.Box{
		box(1, 1, 25, 149),
		box(25, 1, 50, 149),
		box(50, 1, 75, 149),
		box(75, 1, 99, 149),
	}
	merges := []geom.Box{
		box(0, 0, 50, 25),
		box(50, 0, 99, 25),
	}
	semantics := table.RegionSemantics{
		ColumnHeaders: []geom.Box{box(0, 0, 99, 25)},
		RowHeaders:    []geom.Box{box(0, 0, 50, 150)},
		RowSections:   []geom.Box{box(1, 75, 99, 99)},
	}

	data := table.FromRegions(tableBox, rows, cols, merges, semantics)

	require.Equal(t, 4, data.NumCols)
	require.Equal(t, 6, data.NumRows)
	require.Len(t, data.Cells, 18)

	requireBox(t, box(1, 1, 50, 25), *data.Cells[0].Box)
	require.Equal(t, 2, data.Cells[0].ColSpan)
	require.True(t, data.Cells[0].ColumnHeader)
	require.True(t, data.Cells[1].ColumnHeader)
	require.True(t, data.Cells[10].RowHeader)
	require.True(t, data.Cells[12].RowSection)
	requireBox(t, box(75, 100, 99, 149), *data.Cells[17].Box)
}

func TestFromRegionsFallsBackToSingleCellWhenStructureIsMissing(t *testing.T) {
	t.Parallel()

	tableBox := box(10, 20, 110, 120)

	data := table.FromRegions(tableBox, nil, nil, nil, table.RegionSemantics{})

	require.Equal(t, 1, data.NumRows)
	require.Equal(t, 1, data.NumCols)
	require.Len(t, data.Cells, 1)
	requireBox(t, tableBox, *data.Cells[0].Box)
	require.Equal(t, 1, data.Cells[0].RowSpan)
	require.Equal(t, 1, data.Cells[0].ColSpan)
}

func box(l, t, r, b float64) geom.Box {
	return geom.Box{L: l, T: t, R: r, B: b, Origin: geom.TopLeft}
}

func requireBox(t *testing.T, want, got geom.Box) {
	t.Helper()

	require.Equal(t, want.Origin, got.Origin)
	require.InDelta(t, want.L, got.L, 0.001)
	require.InDelta(t, want.T, got.T, 0.001)
	require.InDelta(t, want.R, got.R, 0.001)
	require.InDelta(t, want.B, got.B, 0.001)
}
