package benchdp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const defaultToolTimeout = 10 * time.Minute

type ToolRunResult struct {
	Markdown string        `json:"markdown,omitempty"`
	Duration time.Duration `json:"duration,omitempty"`
}

func ExpandCommand(argv []string, inputPath, outputPath string) []string {
	out := make([]string, 0, len(argv))
	for _, arg := range argv {
		arg = strings.ReplaceAll(arg, "{{input}}", inputPath)
		arg = strings.ReplaceAll(arg, "{{output}}", outputPath)
		out = append(out, arg)
	}
	return out
}

func RunTool(ctx context.Context, tool ToolConfig, inputPath, outputPath string) (ToolRunResult, error) {
	mode := tool.OutputMode
	if mode == "" {
		mode = OutputStdout
	}
	if mode != OutputStdout && mode != OutputFile {
		return ToolRunResult{}, fmt.Errorf("%s: unsupported output mode %q", tool.Name, tool.OutputMode)
	}
	if len(tool.Command) == 0 {
		return ToolRunResult{}, fmt.Errorf("%s: missing command", tool.Name)
	}
	if mode == OutputFile && outputPath == "" {
		return ToolRunResult{}, fmt.Errorf("%s: output file mode requires output path", tool.Name)
	}

	runCtx, cancel := context.WithTimeout(ctx, timeoutFor(tool))
	defer cancel()

	argv := ExpandCommand(tool.Command, inputPath, outputPath)
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	if err := cmd.Run(); err != nil {
		return ToolRunResult{}, commandError(tool.Name, err, stderr.String())
	}
	duration := time.Since(start)

	if mode == OutputFile {
		data, err := os.ReadFile(outputPath)
		if err != nil {
			return ToolRunResult{}, fmt.Errorf("%s: read output file: %w", tool.Name, err)
		}
		return ToolRunResult{Markdown: string(data), Duration: duration}, nil
	}
	return ToolRunResult{Markdown: stdout.String(), Duration: duration}, nil
}

func ToolVersion(ctx context.Context, tool ToolConfig) (string, error) {
	if len(tool.VersionCommand) == 0 {
		return "", nil
	}
	runCtx, cancel := context.WithTimeout(ctx, timeoutFor(tool))
	defer cancel()

	cmd := exec.CommandContext(runCtx, tool.VersionCommand[0], tool.VersionCommand[1:]...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", commandError(tool.Name, err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

func timeoutFor(tool ToolConfig) time.Duration {
	if tool.TimeoutSeconds <= 0 {
		return defaultToolTimeout
	}
	return time.Duration(tool.TimeoutSeconds) * time.Second
}

func commandError(name string, err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return fmt.Errorf("%s: %w", name, err)
	}
	return fmt.Errorf("%s: %w: %s", name, err, stderr)
}
