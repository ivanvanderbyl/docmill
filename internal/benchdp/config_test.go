package benchdp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadToolConfigParsesTools(t *testing.T) {
	t.Parallel()

	config, err := LoadToolConfig([]byte(`{
		"tools": [
			{
				"name": "docmill",
				"version_command": ["docmill", "--version"],
				"command": ["docmill", "{{input}}"],
				"output_mode": "stdout",
				"timeout_seconds": 30
			}
		]
	}`))

	require.NoError(t, err)
	require.Len(t, config.Tools, 1)
	require.Equal(t, "docmill", config.Tools[0].Name)
	require.Equal(t, []string{"docmill", "{{input}}"}, config.Tools[0].Command)
	require.Equal(t, OutputStdout, config.Tools[0].OutputMode)
	require.Equal(t, 30, config.Tools[0].TimeoutSeconds)
}

func TestValidateRequiredToolsReportsMissingCompetitors(t *testing.T) {
	t.Parallel()

	config := ToolConfigFile{
		Tools: []ToolConfig{
			{Name: "docmill", Command: []string{"docmill", "{{input}}"}, OutputMode: OutputStdout},
			{Name: "docling", Command: []string{"docling", "{{input}}"}, OutputMode: OutputStdout},
		},
	}

	err := ValidateRequiredTools(config, RequiredToolNames)

	require.Error(t, err)
	require.Contains(t, err.Error(), "opendataloader-hybrid")
	require.NotContains(t, err.Error(), "docmill")
}

func TestValidateRequiredToolsAcceptsRequestedBenchmarkTools(t *testing.T) {
	t.Parallel()

	config := ToolConfigFile{}
	for _, name := range RequiredToolNames {
		config.Tools = append(config.Tools, ToolConfig{
			Name:       name,
			Command:    []string{name, "{{input}}"},
			OutputMode: OutputStdout,
		})
	}

	require.NoError(t, ValidateRequiredTools(config, RequiredToolNames))
}

func TestExampleToolsConfigContainsRequiredBenchmarkTools(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "benchmarks", "dpbench", "tools.example.json"))
	require.NoError(t, err)

	config, err := LoadToolConfig(data)
	require.NoError(t, err)
	require.NoError(t, ValidateRequiredTools(config, RequiredToolNames))
}
