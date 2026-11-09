package parser_test

import (
	"context"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	"github.com/stretchr/testify/require"
)

func TestAttachmentsPublicAPI(t *testing.T) {
	d, err := parser.NewBackend().OpenBytes(context.Background(), singlePagePDF(t, ""))
	require.NoError(t, err)
	native, ok := d.(*parser.Document)
	require.True(t, ok)
	attachments, err := native.Attachments(context.Background())
	require.NoError(t, err)
	require.Empty(t, attachments)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = native.Attachments(ctx)
	require.ErrorIs(t, err, context.Canceled)
}
