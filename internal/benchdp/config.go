package benchdp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	OutputStdout = "stdout"
	OutputFile   = "file"
)

var RequiredToolNames = []string{
	"docmill",
	"docling",
	"opendataloader",
	"markitdown",
	"pymupdf4llm",
	"opendataloader-hybrid",
	"liteparse",
	"pypdf",
	"pdf-inspector",
}

type ToolConfigFile struct {
	Tools []ToolConfig `json:"tools"`
}

type ToolConfig struct {
	Name           string   `json:"name"`
	VersionCommand []string `json:"version_command,omitempty"`
	Command        []string `json:"command"`
	OutputMode     string   `json:"output_mode"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

func LoadToolConfig(data []byte) (ToolConfigFile, error) {
	var config ToolConfigFile
	if err := json.Unmarshal(data, &config); err != nil {
		return ToolConfigFile{}, err
	}
	for i, tool := range config.Tools {
		if strings.TrimSpace(tool.Name) == "" {
			return ToolConfigFile{}, fmt.Errorf("tool %d missing name", i)
		}
		if len(tool.Command) == 0 {
			return ToolConfigFile{}, fmt.Errorf("tool %q missing command", tool.Name)
		}
		if tool.OutputMode == "" {
			config.Tools[i].OutputMode = OutputStdout
			continue
		}
		if tool.OutputMode != OutputStdout && tool.OutputMode != OutputFile {
			return ToolConfigFile{}, fmt.Errorf("tool %q unsupported output mode %q", tool.Name, tool.OutputMode)
		}
	}
	return config, nil
}

func ValidateRequiredTools(config ToolConfigFile, required []string) error {
	present := make(map[string]bool, len(config.Tools))
	for _, tool := range config.Tools {
		present[strings.ToLower(tool.Name)] = true
	}

	var missing []string
	for _, name := range required {
		if !present[strings.ToLower(name)] {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return fmt.Errorf("tool config missing required tools: %s", strings.Join(missing, ", "))
}
