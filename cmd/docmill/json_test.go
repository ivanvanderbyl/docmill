package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunJSONRendersParagraphsAndOTSLTables(t *testing.T) {
	t.Parallel()

	input := `{
		"items": [
			{"type": "paragraph", "text": "Intro text"},
			{"type": "table", "otsl": "<ched>Name</ched><ched>Value</ched><nl><fcel>Foo</fcel><fcel>42</fcel><nl>"}
		]
	}`
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := runJSON(strings.NewReader(input), &stdout, &stderr)

	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Equal(t, "Intro text\n\n| Name | Value |\n| ---- | ----: |\n| Foo  |    42 |\n", stdout.String())
}

func TestRunJSONReportsInvalidJSON(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := runJSON(strings.NewReader("{"), &stdout, &stderr)

	require.Error(t, err)
	require.Empty(t, stdout.String())
	require.Contains(t, stderr.String(), "invalid document JSON")
}
