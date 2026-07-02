package main

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	"github.com/stretchr/testify/require"
)

func TestRunConvertReportsMissingArgument(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := runConvert(context.Background(), nil, &stdout, &stderr)

	require.Error(t, err)
	require.Empty(t, stdout.String())
	require.Contains(t, stderr.String(), "usage:")
}

func TestRunConvertReportsUnreadableFile(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := runConvert(context.Background(), []string{"/no/such/file.pdf"}, &stdout, &stderr)

	require.Error(t, err)
	require.Empty(t, stdout.String())
	require.Contains(t, stderr.String(), "read PDF")
}

func TestRunConvertAcceptsArgSeparatorBeforePath(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := runConvert(context.Background(), []string{"--", "/no/such/file.pdf"}, &stdout, &stderr)

	require.Error(t, err)
	require.Empty(t, stdout.String())
	require.Contains(t, stderr.String(), "read PDF")
}

func TestNewConvertBackendUsesNativePDFium(t *testing.T) {
	t.Parallel()

	backend, err := newConvertBackend()
	require.NoError(t, err)
	defer backend.Close()

	require.IsType(t, &parser.Backend{}, backend)
}

func TestRunConvertExtractsMarkdownFromFixture(t *testing.T) {
	t.Parallel()

	fixture := "../../.upstream-docling/tests/data/pdf/normal_4pages.pdf"
	if _, err := os.Stat(fixture); os.IsNotExist(err) {
		alt := "../../upstream-docling/tests/data/pdf/normal_4pages.pdf"
		if _, altErr := os.Stat(alt); os.IsNotExist(altErr) {
			t.Skipf("fixture %s is not available", fixture)
		}
		fixture = alt
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := runConvert(context.Background(), []string{fixture}, &stdout, &stderr)
	require.NoError(t, err)

	require.Empty(t, stderr.String())
	require.Contains(t, stdout.String(), "코로나-19")
}
