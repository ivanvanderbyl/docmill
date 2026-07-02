package benchdp

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExpandCommandReplacesInputAndOutputPlaceholders(t *testing.T) {
	t.Parallel()

	got := ExpandCommand([]string{"tool", "--in", "{{input}}", "--out={{output}}"}, "input.pdf", "output.md")

	require.Equal(t, []string{"tool", "--in", "input.pdf", "--out=output.md"}, got)
}

func TestRunToolCapturesStdout(t *testing.T) {
	t.Setenv("BENCHDP_HELPER_PROCESS", "stdout")

	result, err := RunTool(context.Background(), ToolConfig{
		Name:       "fake-stdout",
		Command:    []string{os.Args[0], "-test.run=TestHelperProcess", "--", "{{input}}"},
		OutputMode: OutputStdout,
	}, "sample.pdf", "")

	require.NoError(t, err)
	require.Equal(t, "converted sample.pdf\n", result.Markdown)
	require.Greater(t, result.Duration.Seconds(), 0.0)
}

func TestRunToolReadsOutputFile(t *testing.T) {
	t.Setenv("BENCHDP_HELPER_PROCESS", "file")

	outputPath := t.TempDir() + "/out.md"
	result, err := RunTool(context.Background(), ToolConfig{
		Name:       "fake-file",
		Command:    []string{os.Args[0], "-test.run=TestHelperProcess", "--", "{{input}}", "{{output}}"},
		OutputMode: OutputFile,
	}, "sample.pdf", outputPath)

	require.NoError(t, err)
	require.Equal(t, "file converted sample.pdf\n", result.Markdown)
}

func TestToolVersionCapturesVersionCommandOutput(t *testing.T) {
	t.Setenv("BENCHDP_HELPER_PROCESS", "version")

	version, err := ToolVersion(context.Background(), ToolConfig{
		Name:           "fake-version",
		VersionCommand: []string{os.Args[0], "-test.run=TestHelperProcess"},
	})

	require.NoError(t, err)
	require.Equal(t, "fake-version 1.2.3", version)
}

func TestRunToolRejectsUnknownOutputMode(t *testing.T) {
	t.Parallel()

	_, err := RunTool(context.Background(), ToolConfig{
		Name:       "bad",
		Command:    []string{os.Args[0], "-test.run=TestHelperProcess"},
		OutputMode: "database",
	}, "sample.pdf", "")

	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported output mode")
}

func TestHelperProcess(t *testing.T) {
	mode := os.Getenv("BENCHDP_HELPER_PROCESS")
	if mode == "" {
		return
	}

	switch mode {
	case "stdout":
		args := os.Args
		input := args[len(args)-1]
		_, _ = os.Stdout.WriteString("converted " + input + "\n")
	case "file":
		args := os.Args
		input := args[len(args)-2]
		output := args[len(args)-1]
		require.NoError(t, os.WriteFile(output, []byte("file converted "+input+"\n"), 0o644))
	case "version":
		_, _ = os.Stdout.WriteString("fake-version 1.2.3\n")
	default:
		t.Fatalf("unsupported helper mode %q", mode)
	}
	os.Exit(0)
}
