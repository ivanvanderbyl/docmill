package benchdp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEvaluateMarkdownScoresExactMatchAsPerfect(t *testing.T) {
	t.Parallel()

	groundTruth := "# Quarterly Results\n\nRevenue increased in Q4.\n\n| Region | Revenue |\n| --- | ---: |\n| AU | 42 |\n"

	scores := EvaluateMarkdown(groundTruth, groundTruth)

	require.Equal(t, Scores{
		ExtractionAccuracy: 1,
		ReadingOrderNID:    1,
		TableStructureTEDS: 1,
		HeadingLevelMHS:    1,
	}, scores)
}

func TestEvaluateMarkdownUsesTokenF1ForExtractionAccuracy(t *testing.T) {
	t.Parallel()

	scores := EvaluateMarkdown("alpha beta gamma delta", "alpha beta")

	require.InEpsilon(t, 0.6667, scores.ExtractionAccuracy, 0.001)
}

func TestEvaluateMarkdownPenalisesReadingOrderChangesSeparately(t *testing.T) {
	t.Parallel()

	scores := EvaluateMarkdown("first block\n\nsecond block\n\nthird block", "second block\n\nfirst block\n\nthird block")

	require.Equal(t, 1.0, scores.ExtractionAccuracy)
	require.Less(t, scores.ReadingOrderNID, 1.0)
	require.Greater(t, scores.ReadingOrderNID, 0.0)
}

func TestEvaluateMarkdownScoresTableStructureFromMarkdownRows(t *testing.T) {
	t.Parallel()

	groundTruth := "| A | B |\n| --- | --- |\n| 1 | 2 |\n| 3 | 4 |\n"
	predicted := "| A | B |\n| --- | --- |\n| 1 | 2 |\n"

	scores := EvaluateMarkdown(groundTruth, predicted)

	require.Less(t, scores.TableStructureTEDS, 1.0)
	require.Greater(t, scores.TableStructureTEDS, 0.0)
}

func TestEvaluateMarkdownScoresHeadingLevels(t *testing.T) {
	t.Parallel()

	scores := EvaluateMarkdown("# Title\n\n## Method\n", "# Title\n\n### Method\n")

	require.InEpsilon(t, 0.5, scores.HeadingLevelMHS, 0.001)
}

func TestEvaluateMarkdownReadingOrderIgnoresAllHeadingLevels(t *testing.T) {
	t.Parallel()

	scores := EvaluateMarkdown("## Method\n\nbody", "### Method\n\nbody")

	require.Equal(t, 1.0, scores.ReadingOrderNID)
	require.Equal(t, 1.0, scores.ExtractionAccuracy)
	require.Equal(t, 0.0, scores.HeadingLevelMHS)
}
