package table

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/stretchr/testify/require"
)

func TestCellStartsTableCaption(t *testing.T) {
	t.Parallel()
	yes := []string{"[Table 2.2.4.A] Evaluations", "Table 3: Results", "TABLE 12", "Tab. 4 — overview"}
	no := []string{"Tablet computers are common", "see the table below", "Notable findings", "", "Figure 2"}
	for _, s := range yes {
		require.Truef(t, cellStartsTableCaption(s), "%q should be a table caption", s)
	}
	for _, s := range no {
		require.Falsef(t, cellStartsTableCaption(s), "%q should not be a table caption", s)
	}
}

func TestTableHasNearbyTableCaption(t *testing.T) {
	t.Parallel()
	box := geom.Box{L: 100, T: 100, R: 400, B: 200, Origin: geom.TopLeft}
	caption := func(l, top, r, b float64) page.TextCell {
		return page.TextCell{Index: 1, Text: "[Table 2.2.4.A] Evaluations", Box: geom.Box{L: l, T: top, R: r, B: b, Origin: geom.TopLeft}}
	}

	// Caption just below the table is detected.
	require.True(t, tableHasNearbyTableCaption(box, []page.TextCell{caption(100, 210, 400, 222)}))
	// Caption just above the table is detected.
	require.True(t, tableHasNearbyTableCaption(box, []page.TextCell{caption(100, 70, 400, 82)}))
	// Caption far away vertically is not.
	require.False(t, tableHasNearbyTableCaption(box, []page.TextCell{caption(100, 800, 400, 812)}))
	// Caption horizontally disjoint is not.
	require.False(t, tableHasNearbyTableCaption(box, []page.TextCell{caption(900, 210, 1100, 222)}))
	// A non-caption cell beside the table does not spare it.
	require.False(t, tableHasNearbyTableCaption(box, []page.TextCell{
		{Index: 2, Text: "ordinary body text after the region", Box: geom.Box{L: 100, T: 210, R: 400, B: 222, Origin: geom.TopLeft}},
	}))
}
