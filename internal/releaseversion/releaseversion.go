// Package releaseversion calculates stable semantic versions for releases.
package releaseversion

import (
	"fmt"
	"regexp"
	"strconv"
)

var stableTagPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// Next returns the next stable tag after current. Release labels select the
// largest requested semantic-version bump; without one, Next bumps patch.
func Next(current string, labels []string) (string, error) {
	matches := stableTagPattern.FindStringSubmatch(current)
	if matches == nil {
		return "", fmt.Errorf("current tag %q is not a stable vMAJOR.MINOR.PATCH tag", current)
	}

	parts := make([]int, 3)
	for i, value := range matches[1:] {
		part, err := strconv.Atoi(value)
		if err != nil {
			return "", fmt.Errorf("parse current tag %q: %w", current, err)
		}
		parts[i] = part
	}

	bump := "patch"
	for _, label := range labels {
		switch label {
		case "release:major":
			bump = "major"
		case "release:minor":
			if bump != "major" {
				bump = "minor"
			}
		}
	}

	switch bump {
	case "major":
		parts[0]++
		parts[1], parts[2] = 0, 0
	case "minor":
		parts[1]++
		parts[2] = 0
	default:
		parts[2]++
	}

	return fmt.Sprintf("v%d.%d.%d", parts[0], parts[1], parts[2]), nil
}
