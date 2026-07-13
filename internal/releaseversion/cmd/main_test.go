package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunPrintsNextVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "default patch", args: []string{"-current", "v2.0.1"}, want: "v2.0.2\n"},
		{name: "labels", args: []string{"-current", "v2.0.1", "-labels", "documentation,release:minor"}, want: "v2.1.0\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			if code := run(tt.args, &stdout, &stderr); code != 0 {
				t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
			}
			if got := stdout.String(); got != tt.want {
				t.Errorf("stdout = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-current", "not-a-tag"}, &stdout, &stderr); code == 0 {
		t.Fatal("run() code = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "not a stable") {
		t.Errorf("stderr = %q, want a useful invalid-tag error", stderr.String())
	}
}
