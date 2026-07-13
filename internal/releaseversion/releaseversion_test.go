package releaseversion

import "testing"

func TestNext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current string
		labels  []string
		want    string
	}{
		{name: "patch by default", current: "v2.0.1", want: "v2.0.2"},
		{name: "explicit patch", current: "v2.3.4", labels: []string{"release:patch"}, want: "v2.3.5"},
		{name: "minor", current: "v2.3.4", labels: []string{"release:minor"}, want: "v2.4.0"},
		{name: "major", current: "v2.3.4", labels: []string{"release:major"}, want: "v3.0.0"},
		{name: "unrelated labels default to patch", current: "v2.3.4", labels: []string{"documentation"}, want: "v2.3.5"},
		{name: "largest bump wins", current: "v2.3.4", labels: []string{"release:patch", "release:major", "release:minor"}, want: "v3.0.0"},
		{name: "large components", current: "v12.99.999", labels: []string{"release:minor"}, want: "v12.100.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Next(tt.current, tt.labels)
			if err != nil {
				t.Fatalf("Next() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Next() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNextRejectsInvalidStableTags(t *testing.T) {
	t.Parallel()

	for _, current := range []string{"", "2.0.1", "v2", "v2.0", "v2.0.1-rc.1", "v2.0.1.0", "v02.0.1"} {
		t.Run(current, func(t *testing.T) {
			t.Parallel()
			if _, err := Next(current, nil); err == nil {
				t.Errorf("Next(%q, nil) error = nil, want an invalid-tag error", current)
			}
		})
	}
}
