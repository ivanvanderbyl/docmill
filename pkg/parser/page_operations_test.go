package parser_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	"github.com/stretchr/testify/require"
)

func TestPageOperationsPublicAPI(t *testing.T) {
	ctx := context.Background()
	input := singlePagePDF(t, "")
	merged, err := parser.MergePDFs(ctx, input, input, input)
	require.NoError(t, err)
	opened, err := parser.NewBackend().OpenBytes(ctx, merged)
	require.NoError(t, err)
	d := opened.(*parser.Document)
	count, err := d.PageCount(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, count)
	var out bytes.Buffer
	require.NoError(t, d.WritePageRange(ctx, &out, 1, 3))
	selected, err := parser.NewBackend().OpenBytes(ctx, out.Bytes())
	require.NoError(t, err)
	count, err = selected.PageCount(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, count)
	for _, bounds := range [][2]int{{-1, 1}, {0, 4}, {2, 2}, {3, 2}} {
		out.Reset()
		require.Error(t, d.WritePageRange(ctx, &out, bounds[0], bounds[1]))
		require.Empty(t, out.Bytes())
	}
	out.Reset()
	require.NoError(t, parser.WritePageSelections(ctx, &out, []parser.PageSelection{{Document: d, Pages: []int{2, 0}}, {Document: d, Pages: []int{1}}}))
	selected, err = parser.NewBackend().OpenBytes(ctx, out.Bytes())
	require.NoError(t, err)
	count, err = selected.PageCount(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, count)
}

func TestMergePDFsRejectsInvalidInputs(t *testing.T) {
	for _, inputs := range [][][]byte{nil, {[]byte("bad PDF")}, {singlePagePDF(t, ""), []byte("bad PDF")}} {
		result, err := parser.MergePDFs(context.Background(), inputs...)
		require.Error(t, err)
		require.Nil(t, result)
	}
	var out bytes.Buffer
	var nilDocument *parser.Document
	require.Error(t, nilDocument.WritePages(context.Background(), &out, []int{0}))
	require.Error(t, parser.WritePageSelections(context.Background(), &out, []parser.PageSelection{{Document: nil}}))
	require.Empty(t, out.Bytes())
}
