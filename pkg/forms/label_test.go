package forms

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ivanvanderbyl/docmill/pkg/geom"
	"github.com/ivanvanderbyl/docmill/pkg/page"
)

func textCell(text string, l, t, r, b float64) page.TextCell {
	return page.TextCell{Text: text, Box: geom.Box{L: l, T: t, R: r, B: b, Origin: geom.TopLeft}}
}

func box(l, t, r, b float64) geom.Box {
	return geom.Box{L: l, T: t, R: r, B: b, Origin: geom.TopLeft}
}

func TestLabelsTooltipWinsOverGeometry(t *testing.T) {
	t.Parallel()

	widgets := []Widget{{Tooltip: "Full legal name", Type: "Tx", Box: box(200, 100, 400, 120)}}
	lines := []page.TextCell{textCell("Name:", 60, 102, 190, 116)}

	got := Detect(widgets, lines, nil).Labels
	require.Equal(t, []Label{{Text: "Full legal name", Source: SourceTooltip}}, got)
}

func TestLabelsTextFieldPrefersLeftCaption(t *testing.T) {
	t.Parallel()

	widgets := []Widget{{Type: "Tx", Box: box(200, 100, 400, 120)}}
	lines := []page.TextCell{
		textCell("Your IRD number", 60, 102, 190, 116), // left, gap 10
		textCell("Section heading", 200, 60, 300, 75),  // above, gap 25
	}

	got := Detect(widgets, lines, nil).Labels
	require.Equal(t, "Your IRD number", got[0].Text)
	require.Equal(t, SourceCaptionLeft, got[0].Source)
}

func TestLabelsCheckboxesClaimTheirRightWord(t *testing.T) {
	t.Parallel()

	widgets := []Widget{
		{Type: "Btn", Box: box(300, 100, 311, 111)},
		{Type: "Btn", Box: box(370, 100, 381, 111)},
	}
	words := []page.TextCell{
		textCell("Yes", 315, 100, 335, 111),
		textCell("No", 385, 100, 400, 111),
	}
	lines := []page.TextCell{textCell("Are you a member?", 60, 100, 290, 111)}

	got := Detect(widgets, lines, words).Labels
	require.Equal(t, "Yes", got[0].Text)
	require.Equal(t, SourceCaptionRight, got[0].Source)
	require.Equal(t, "No", got[1].Text)
	require.Equal(t, SourceCaptionRight, got[1].Source)
}

func TestLabelsRadioFlagBeatsShapeHeuristic(t *testing.T) {
	t.Parallel()

	// 30pt square: too big for the checkbox shape fallback, but the radio
	// flag marks it checkable, so the caption comes from the right.
	widgets := []Widget{{Type: "Btn", Flags: flagRadio, Box: box(300, 100, 330, 130)}}
	words := []page.TextCell{textCell("Male", 336, 108, 370, 122)}

	got := Detect(widgets, nil, words).Labels
	require.Equal(t, "Male", got[0].Text)
	require.Equal(t, SourceCaptionRight, got[0].Source)
}

func TestLabelsCombFieldPrefersBelowCaption(t *testing.T) {
	t.Parallel()

	widgets := []Widget{{Type: "Tx", Flags: flagComb, Box: box(60, 100, 500, 120)}}
	lines := []page.TextCell{
		textCell("First names", 60, 124, 120, 134),      // below, gap 4
		textCell("Previous caption", 60, 80, 150, 92),   // above, gap 8
		textCell("Unrelated column", 60, 200, 150, 214), // far below, over cap
	}

	got := Detect(widgets, lines, nil).Labels
	require.Equal(t, "First names", got[0].Text)
	require.Equal(t, SourceCaptionBelow, got[0].Source)
}

func TestLabelsSignaturePrefersBelowCaption(t *testing.T) {
	t.Parallel()

	widgets := []Widget{{Type: "Sig", Box: box(57, 756, 257, 782)}}
	lines := []page.TextCell{
		textCell("I declare this is true.", 53, 730, 400, 744), // above, gap 12
		textCell("Signature", 57, 787, 100, 796),               // below, gap 5
	}

	got := Detect(widgets, lines, nil).Labels
	require.Equal(t, "Signature", got[0].Text)
	require.Equal(t, SourceCaptionBelow, got[0].Source)
}

func TestLabelsAdjacentCombsShareOneCaption(t *testing.T) {
	t.Parallel()

	// A phone number split across three comb widgets with one caption under
	// the first: the cluster labels all three members.
	widgets := []Widget{
		{Type: "Tx", Flags: flagComb, Box: box(100, 100, 150, 120)},
		{Type: "Tx", Flags: flagComb, Box: box(157, 100, 200, 120)},
		{Type: "Tx", Flags: flagComb, Box: box(207, 100, 260, 120)},
	}
	lines := []page.TextCell{textCell("Day", 100, 124, 115, 132)}

	got := Detect(widgets, lines, nil).Labels
	for i := range widgets {
		require.Equal(t, "Day", got[i].Text, "widget %d", i)
		require.Equal(t, SourceCaptionBelow, got[i].Source, "widget %d", i)
	}
}

func TestLabelsExclusiveAssignmentResolvesContestedCaption(t *testing.T) {
	t.Parallel()

	// "Day" sits between two comb rows: below row A (its true owner) and
	// above row B. Local scoring would give both rows "Day"; exclusivity
	// hands it to A (higher score) and B falls back to its left caption.
	widgets := []Widget{
		{Type: "Tx", Flags: flagComb, Box: box(100, 100, 300, 120)},
		{Type: "Tx", Flags: flagComb, Box: box(100, 136, 300, 156)},
	}
	lines := []page.TextCell{
		textCell("Day", 100, 124, 115, 134),
		textCell("Your email", 20, 138, 58, 152), // left of row B, gap 42
	}

	got := Detect(widgets, lines, nil).Labels
	require.Equal(t, "Day", got[0].Text)
	require.Equal(t, SourceCaptionBelow, got[0].Source)
	require.Equal(t, "Your email", got[1].Text)
	require.Equal(t, SourceCaptionLeft, got[1].Source)
}

func TestLabelsOcclusionBlocksCaptionBehindNeighbour(t *testing.T) {
	t.Parallel()

	// "Name" belongs to Y; X must not reach across Y to claim it. The 14pt
	// gap between X and Y keeps them out of one cluster (adjacency cap is
	// 12pt), so X's only path to "Name" is across Y — blocked by occlusion.
	widgets := []Widget{
		{Type: "Tx", Box: box(204, 100, 304, 120)}, // X
		{Type: "Tx", Box: box(100, 100, 190, 120)}, // Y
	}
	lines := []page.TextCell{textCell("Name", 20, 102, 95, 118)}

	got := Detect(widgets, lines, nil).Labels
	require.Equal(t, "", got[0].Text)
	require.Equal(t, SourceNone, got[0].Source)
	require.Equal(t, "Name", got[1].Text)
	require.Equal(t, SourceCaptionLeft, got[1].Source)
}

func TestLabelsLengthPenaltyRejectsFootnote(t *testing.T) {
	t.Parallel()

	widgets := []Widget{{Type: "Tx", Flags: flagComb, Box: box(100, 100, 500, 120)}}
	lines := []page.TextCell{
		textCell("If you give an email address you may receive information by email", 100, 124, 480, 134),
		textCell("Your email address", 10, 102, 95, 118),
	}

	got := Detect(widgets, lines, nil).Labels
	require.Equal(t, "Your email address", got[0].Text)
	require.Equal(t, SourceCaptionLeft, got[0].Source)
}

func TestLabelsPushbuttonStaysUnlabelled(t *testing.T) {
	t.Parallel()

	widgets := []Widget{{Type: "Btn", Flags: flagPushbutton, Box: box(100, 100, 200, 130)}}
	lines := []page.TextCell{textCell("Reset the form", 210, 105, 300, 125)}

	got := Detect(widgets, lines, nil).Labels
	require.Equal(t, []Label{{}}, got)
}

func TestLabelsOnStateFallbackForIsolatedCheckbox(t *testing.T) {
	t.Parallel()

	widgets := []Widget{
		{Type: "Btn", OnState: "PostalOnly", Box: box(100, 100, 111, 111)},
		{Type: "Btn", OnState: "A", Box: box(100, 200, 111, 211)},
	}

	got := Detect(widgets, nil, nil).Labels
	require.Equal(t, "PostalOnly", got[0].Text)
	require.Equal(t, SourceState, got[0].Source)
	require.Equal(t, "", got[1].Text, "generic on-state names are not labels")
	require.Equal(t, SourceNone, got[1].Source)
}

func TestDetectGroupContextFromNumberedAnchors(t *testing.T) {
	t.Parallel()

	widgets := []Widget{
		{Type: "Tx", Box: box(200, 60, 400, 80)},   // above the first anchor
		{Type: "Tx", Box: box(200, 120, 400, 140)}, // under question 5
		{Type: "Tx", Box: box(200, 220, 400, 240)}, // under question 6
	}
	lines := []page.TextCell{
		textCell("5.", 50, 100, 60, 110),
		textCell("Your name", 70, 100, 130, 110),
		textCell("6. Your postal address", 50, 200, 190, 210),
	}

	got := Detect(widgets, lines, nil)
	require.Equal(t, "", got.Labels[0].Group)
	require.Equal(t, "Your name", got.Labels[1].Group)
	require.Equal(t, "Your postal address", got.Labels[2].Group)
	require.Equal(t, []Group{
		{Kind: GroupQuestion, Label: "Your name", Fields: []int{1}},
		{Kind: GroupQuestion, Label: "Your postal address", Fields: []int{2}},
	}, got.Groups)
}

func TestDetectFieldGroupsCollectSharedNames(t *testing.T) {
	t.Parallel()

	// One checkbox field with two widgets (Yes/No boxes) plus an unrelated
	// singleton: only the shared name forms a field group.
	widgets := []Widget{
		{Name: "Member", Type: "Btn", OnState: "Yes", Box: box(300, 100, 311, 111)},
		{Name: "Email", Type: "Tx", Box: box(100, 200, 300, 220)},
		{Name: "Member", Type: "Btn", OnState: "No", Box: box(370, 100, 381, 111)},
	}

	got := Detect(widgets, nil, nil)
	require.Equal(t, []Group{
		{Kind: GroupField, Name: "Member", Fields: []int{0, 2}},
	}, got.Groups)
}

func TestDetectClusterGroupsCarryTheSharedCaption(t *testing.T) {
	t.Parallel()

	widgets := []Widget{
		{Type: "Tx", Flags: flagComb, Box: box(100, 100, 150, 120)},
		{Type: "Tx", Flags: flagComb, Box: box(157, 100, 200, 120)},
		{Type: "Tx", Flags: flagComb, Box: box(207, 100, 260, 120)},
		{Type: "Tx", Flags: flagComb, Box: box(100, 200, 260, 220)}, // separate row, no cluster
	}
	lines := []page.TextCell{textCell("Day", 100, 124, 115, 132)}

	got := Detect(widgets, lines, nil)
	require.Equal(t, []Group{
		{Kind: GroupCluster, Label: "Day", Fields: []int{0, 1, 2}},
	}, got.Groups)
}

func TestLabelsEmptyInputs(t *testing.T) {
	t.Parallel()

	require.Empty(t, Detect(nil, nil, nil).Labels)
	got := Detect([]Widget{{Type: "Tx", Box: box(0, 0, 10, 10)}}, nil, nil)
	require.Equal(t, []Label{{}}, got.Labels)
	require.Empty(t, got.Groups)
}
