package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDispatchNoArgsPrintsUsage(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := dispatch(context.Background(), []string{"docmill"}, strings.NewReader(""), &stdout, &stderr)

	require.Equal(t, 2, code)
	require.Contains(t, stderr.String(), "usage:")
}

func TestDispatchHelpPrintsCommands(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := dispatch(context.Background(), []string{"docmill", "help"}, strings.NewReader(""), &stdout, &stderr)

	require.Equal(t, 0, code)
	require.Contains(t, stdout.String(), "Commands:")
	require.Contains(t, stdout.String(), "forms export <input.pdf>")
	require.NotContains(t, stdout.String(), "acroform export")
}

func TestDispatchRoutesJSON(t *testing.T) {
	t.Parallel()

	input := `{"items":[{"type":"paragraph","text":"hello"}]}`
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := dispatch(context.Background(), []string{"docmill", "json"}, strings.NewReader(input), &stdout, &stderr)

	require.Equal(t, 0, code)
	require.Contains(t, stdout.String(), "hello")
}

func TestDispatchBareArgRoutesToConvert(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// A bare, non-existent path is treated as a PDF -> convert reports "read PDF".
	code := dispatch(context.Background(), []string{"docmill", "/no/such/file.pdf"}, strings.NewReader(""), &stdout, &stderr)

	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), "read PDF")
}

func TestDispatchArgSeparatorBeforeBarePathRoutesToConvert(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := dispatch(context.Background(), []string{"docmill", "--", "/no/such/file.pdf"}, strings.NewReader(""), &stdout, &stderr)

	require.Equal(t, 1, code)
	require.Empty(t, stdout.String())
	require.Contains(t, stderr.String(), "read PDF")
}

func TestDispatchBenchWithoutCgoOrMissingFileFails(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// Build-agnostic: without -tags pdfium_cgo this returns the requirement
	// message; with it, the missing file fails. Either way the code is 1.
	code := dispatch(context.Background(), []string{"docmill", "bench", "/no/such/file.pdf"}, strings.NewReader(""), &stdout, &stderr)

	require.Equal(t, 1, code)
}
