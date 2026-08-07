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

func TestRunConvertRejectsUnknownFlag(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := runConvert(context.Background(), []string{"-nope", "/no/such/file.pdf"}, &stdout, &stderr)

	require.Error(t, err)
	require.Empty(t, stdout.String())
	require.Contains(t, stderr.String(), "flag provided but not defined")
}

func TestRunConvertAcceptsLearnedLayoutFlagBeforePath(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// The flag parses, so the path is reached and the missing file is the error.
	err := runConvert(context.Background(), []string{"-learned-layout", "/no/such/file.pdf"}, &stdout, &stderr)

	require.Error(t, err)
	require.Empty(t, stdout.String())
	require.Contains(t, stderr.String(), "read PDF")
}

func TestConvertOptionsDefaultMatchesExtractMarkdown(t *testing.T) {
	t.Parallel()

	options := convertOptions(false, false, false)

	require.True(t, options.DetectTables)
	require.True(t, options.ReadingOrder)
	require.True(t, options.DetectStructure)
	require.True(t, options.DetectHeadings)
	// Nothing learned unless asked: the default conversion is unchanged.
	require.False(t, options.ClassifyThenRoute)
	require.False(t, options.LearnedRouting)
	require.False(t, options.LearnedFormulaRouting)
}

func TestConvertOptionsLearnedLayoutEnablesTheWholeLearnedPath(t *testing.T) {
	t.Parallel()

	options := convertOptions(true, false, false)

	// LearnedRouting is only consulted on the rerouted path, and the Formula
	// veto is a separate gate — the flag has to set all three or it silently
	// migrates only some classes.
	require.True(t, options.ClassifyThenRoute)
	require.True(t, options.LearnedRouting)
	require.True(t, options.LearnedFormulaRouting)
	require.True(t, options.DetectHeadings)
	require.True(t, options.DetectStructure)
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

func TestConvertOptionsLearnedColumnsIsIndependent(t *testing.T) {
	// -learned-columns changes table STRUCTURE, -learned-layout changes which
	// regions are what. They must be selectable separately or their effects
	// cannot be measured apart — TEDS moves for one, the class metrics for the
	// other.
	columnsOnly := convertOptions(false, true, false)
	require.True(t, columnsOnly.LearnedColumns)
	require.True(t, columnsOnly.ClassifyThenRoute, "learned columns need the rerouted path for rulings")
	require.False(t, columnsOnly.LearnedRouting)

	layoutOnly := convertOptions(true, false, false)
	require.True(t, layoutOnly.LearnedRouting)
	require.False(t, layoutOnly.LearnedColumns)

	require.False(t, convertOptions(false, false, false).ClassifyThenRoute, "default path must stay untouched")
}

func TestConvertOptionsLearnedRegionsImpliesLearnedLayout(t *testing.T) {
	// The region gate decides which candidate regions stand, and the candidates
	// come from the line model. Enabling it without the line model would gate
	// nothing.
	options := convertOptions(false, false, true)
	require.True(t, options.LearnedRegions)
	require.True(t, options.LearnedRouting)
	require.True(t, options.ClassifyThenRoute)

	require.False(t, convertOptions(true, false, false).LearnedRegions,
		"-learned-layout alone must not enable the region gate")
}
