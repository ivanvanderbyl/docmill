// Package forms associates visible text labels with AcroForm widgets using
// only digital page data: widget rectangles, field dictionaries, and
// positioned text cells. No OCR, no rendering.
//
// The algorithm is the evidence-based cascade from
// docs/research/forms-label-detection.md: an authored /TU tooltip always
// wins; otherwise widgets are labelled geometrically with direction priors
// derived from field flags (checkboxes look right, comb fields look below,
// text fields look left/above), a same-class adjacency clustering pass so a
// caption can label a multi-widget group (a phone number split across comb
// boxes), and a global exclusive assignment so each caption labels exactly
// one field or group (OPAL's interleaving constraint; Adobe's "do not use
// the same text label across multiple fields"). Scored 37/37 on the NZ IRD
// KS2 form against 8% for the folklore nearest-left-else-above rule.
package forms

import (
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
)

// Widget is one AcroForm widget to label.
type Widget struct {
	Name    string   // fully-qualified field name (the /T chain); widgets of one field share it
	Tooltip string   // /TU alternate name; authoritative when present
	Type    string   // field type (/FT): Tx, Btn, Ch, or Sig
	OnState string   // checkbox/radio checked appearance-state name; "" otherwise
	Flags   int      // /Ff field flags (PDF 32000-1 Tables 226/228)
	Box     geom.Box // widget rectangle in top-left-origin page points
}

// Source records where a widget's label came from, in the spirit of
// Chromium autofill's LabelSource provenance enum.
type Source string

const (
	SourceNone         Source = ""              // no label found
	SourceTooltip      Source = "tooltip"       // /TU alternate field name
	SourceCaptionLeft  Source = "caption-left"  // text left of the widget
	SourceCaptionRight Source = "caption-right" // text right of the widget
	SourceCaptionAbove Source = "caption-above" // text above the widget
	SourceCaptionBelow Source = "caption-below" // text below the widget
	SourceState        Source = "state"         // /AP /N on-state name fallback
)

// Label is the labelling result for one widget.
type Label struct {
	Text   string // resolved label; "" when nothing plausible was found
	Source Source // provenance of Text
	Group  string // enclosing question/anchor context; "" when none detected
}

// GroupKind classifies a detected multi-widget structure.
type GroupKind string

const (
	// GroupQuestion is a numbered-question band: every widget between one
	// question anchor ("5. Your name") and the next.
	GroupQuestion GroupKind = "question"
	// GroupField collects the widgets of one logical AcroForm field — e.g.
	// the Yes and No boxes of a single checkbox/radio field. Filling the
	// field means choosing one member's on-state.
	GroupField GroupKind = "field"
	// GroupCluster collects adjacent widgets that share one caption — e.g.
	// a phone number split across comb boxes.
	GroupCluster GroupKind = "cluster"
)

// Group is one detected multi-widget structure on the page.
type Group struct {
	Kind   GroupKind
	Label  string // question text or shared caption; "" for field groups
	Name   string // fully-qualified field name; field groups only
	Fields []int  // widget indices into the Detect input, ascending
}

// Result is the outcome of Detect for one page. Labels has one entry per
// input widget, in input order. Groups lists question bands (anchor order),
// then field groups, then caption clusters (each in first-member order);
// only field/cluster groups with at least two members are reported.
type Result struct {
	Labels []Label
	Groups []Group
}

// Field flag bits, PDF 32000-1 Tables 226 (Btn) and 228 (Tx). Bits are
// numbered 1-32 in the spec, so bit n is mask 1<<(n-1).
const (
	flagRadio      = 1 << 15 // Btn bit 16: radio button
	flagPushbutton = 1 << 16 // Btn bit 17: push button
	flagComb       = 1 << 24 // Tx bit 25: comb / boxed character field
)

// direction of a caption candidate relative to a widget.
type direction string

const (
	dirLeft  direction = "left"
	dirRight direction = "right"
	dirAbove direction = "above"
	dirBelow direction = "below"
)

var captionSources = map[direction]Source{
	dirLeft:  SourceCaptionLeft,
	dirRight: SourceCaptionRight,
	dirAbove: SourceCaptionAbove,
	dirBelow: SourceCaptionBelow,
}

// gapCaps bound how far away a caption may sit, per direction, in points.
// Left captions sit in a label column and may be far; below captions are
// tight by convention (IRD-style forms put them 2-5pt under the boxes).
var gapCaps = map[direction]float64{dirLeft: 130, dirRight: 60, dirAbove: 30, dirBelow: 14}

// Detect labels every widget on one page and reports the page's multi-widget
// structure. lines are the merged line-level text cells and words the
// word-level cells (checkbox option captions are single words; left/above
// captions are whole lines).
func Detect(widgets []Widget, lines, words []page.TextCell) Result {
	result := Result{Labels: make([]Label, len(widgets))}
	if len(widgets) == 0 {
		return result
	}
	labels := result.Labels

	allBoxes := make([]geom.Box, len(widgets))
	for i, w := range widgets {
		allBoxes[i] = w.Box
	}

	// Rung 1: authored tooltips win outright. Tooltip-labelled widgets do
	// not compete for captions but their boxes still occlude.
	seeking := make([]bool, len(widgets))
	for i, w := range widgets {
		if tooltip := strings.TrimSpace(w.Tooltip); tooltip != "" {
			labels[i] = Label{Text: tooltip, Source: SourceTooltip}
			continue
		}
		// Push buttons draw their caption on their own face; side captions
		// belong to neighbouring fields (Adobe auto-detection whitepaper).
		if w.Type == "Btn" && w.Flags&flagPushbutton != 0 {
			continue
		}
		seeking[i] = true
	}

	// Rung 2: geometric captions, assigned exclusively across units.
	clusters := clusterWidgets(widgets, seeking)
	assignCaptions(widgets, lines, words, allBoxes, clusters, labels)

	// Rung 3: checkbox/radio on-state fallback for widgets left unlabelled.
	for i, w := range widgets {
		if seeking[i] && labels[i].Text == "" && isSemanticState(w.OnState) {
			labels[i] = Label{Text: w.OnState, Source: SourceState}
		}
	}

	// Group context is orthogonal to the per-widget label.
	questions := questionGroups(allBoxes, lines)
	for _, q := range questions {
		for _, i := range q.Fields {
			labels[i].Group = q.Label
		}
	}

	result.Groups = append(result.Groups, questions...)
	result.Groups = append(result.Groups, fieldGroups(widgets)...)
	result.Groups = append(result.Groups, clusterGroups(clusters, labels)...)
	return result
}

// fieldGroups collects the widgets of each logical field: multiple widget
// annotations sharing one fully-qualified /T name (radio kids, repeated
// checkboxes).
func fieldGroups(widgets []Widget) []Group {
	byName := make(map[string][]int)
	var names []string
	for i, w := range widgets {
		if w.Name == "" {
			continue
		}
		if len(byName[w.Name]) == 0 {
			names = append(names, w.Name)
		}
		byName[w.Name] = append(byName[w.Name], i)
	}
	var out []Group
	for _, name := range names {
		if members := byName[name]; len(members) > 1 {
			out = append(out, Group{Kind: GroupField, Name: name, Fields: members})
		}
	}
	return out
}

// clusterGroups reports caption-sharing adjacency clusters of two or more
// widgets. The shared caption (possibly empty) is every member's label.
func clusterGroups(clusters [][]int, labels []Label) []Group {
	var out []Group
	for _, members := range clusters {
		if len(members) > 1 {
			out = append(out, Group{Kind: GroupCluster, Label: labels[members[0]].Text, Fields: members})
		}
	}
	return out
}

// unit is one label-seeking entity: a single checkbox/radio widget, or a
// cluster of adjacent same-class text widgets that share one caption.
type unit struct {
	members []int
	cands   []scoredCandidate
}

type scoredCandidate struct {
	pool  int // 0 = lines, 1 = words
	index int // index within the pool
	score float64
	text  string
	dir   direction
}

// assignCaptions scores each cluster's caption candidates and assigns
// captions globally: highest-scoring (unit, caption) pairs first, each unit
// labelled once and each text cell claimable once per pool.
func assignCaptions(widgets []Widget, lines, words []page.TextCell, allBoxes []geom.Box, clusters [][]int, out []Label) {
	var units []unit
	for _, members := range clusters {
		w := widgets[members[0]]
		boxes := make([]geom.Box, 0, len(members))
		inUnit := make(map[int]bool, len(members))
		for _, i := range members {
			boxes = append(boxes, widgets[i].Box)
			inUnit[i] = true
		}
		blockers := make([]geom.Box, 0, len(allBoxes)-len(members))
		for i, b := range allBoxes {
			if !inUnit[i] {
				blockers = append(blockers, b)
			}
		}

		pool, cells := 0, lines
		if widgetClass(w) == classCheck {
			pool, cells = 1, words
		}
		box := geom.EnclosingBox(boxes...)
		priors := classPriors(widgetClass(w))
		var cands []scoredCandidate
		for _, c := range candidatesFor(box, cells, blockers) {
			text := strings.TrimSpace(c.cell.Text)
			cands = append(cands, scoredCandidate{
				pool:  pool,
				index: c.index,
				score: priors[c.dir] / (1 + c.gap/12) / (1 + c.mis/24) * labelLikeness(text),
				text:  text,
				dir:   c.dir,
			})
		}
		units = append(units, unit{members: members, cands: cands})
	}

	type pairing struct {
		unit int
		cand scoredCandidate
	}
	var pairs []pairing
	for u, un := range units {
		for _, c := range un.cands {
			pairs = append(pairs, pairing{unit: u, cand: c})
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].cand.score > pairs[j].cand.score })

	labelled := make([]bool, len(units))
	claimed := make(map[[2]int]bool, len(pairs))
	for _, p := range pairs {
		key := [2]int{p.cand.pool, p.cand.index}
		if labelled[p.unit] || claimed[key] {
			continue
		}
		labelled[p.unit] = true
		claimed[key] = true
		for _, i := range units[p.unit].members {
			out[i] = Label{Text: p.cand.text, Source: captionSources[p.cand.dir]}
		}
	}
}

// clusterWidgets unions adjacent same-class caption-seeking text/signature
// widgets into visual field groups: side by side on one band (a phone number
// split across comb boxes) or tightly stacked rows (a two-row email field).
// Checkboxes and radios stay singletons — each carries its own caption.
func clusterWidgets(widgets []Widget, seeking []bool) [][]int {
	parent := make([]int, len(widgets))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}

	clusterable := func(i int) bool {
		w := widgets[i]
		return seeking[i] && (w.Type == "Tx" || w.Type == "Sig") && widgetClass(w) != classCheck
	}
	for i := range widgets {
		if !clusterable(i) {
			continue
		}
		for j := i + 1; j < len(widgets); j++ {
			if !clusterable(j) || widgetClass(widgets[i]) != widgetClass(widgets[j]) {
				continue
			}
			a, b := widgets[i].Box, widgets[j].Box
			sameBand := vOverlap(a, b) > 0.6*math.Min(a.Height(), b.Height())
			hGap := math.Max(a.L, b.L) - math.Min(a.R, b.R)
			stacked := hOverlap(a, b) > 0.8*math.Min(a.Width(), b.Width())
			vGap := math.Max(a.T, b.T) - math.Min(a.B, b.B)
			if (sameBand && hGap >= 0 && hGap <= 12) || (stacked && vGap >= 0 && vGap <= 4) {
				parent[find(i)] = find(j)
			}
		}
	}

	groups := make(map[int][]int)
	var roots []int
	for i := range widgets {
		if !seeking[i] {
			continue
		}
		root := find(i)
		if len(groups[root]) == 0 {
			roots = append(roots, root)
		}
		groups[root] = append(groups[root], i)
	}
	clusters := make([][]int, 0, len(roots))
	for _, root := range roots {
		clusters = append(clusters, groups[root])
	}
	return clusters
}

// Widget classes share a caption-placement convention.
type class string

const (
	classCheck class = "check" // checkbox/radio: caption to the right
	classComb  class = "comb"  // boxed character row: caption below
	classSig   class = "sig"   // signature line: caption below
	classText  class = "text"  // plain text/choice field: caption left/above
)

func widgetClass(w Widget) class {
	switch {
	case w.Type == "Btn" && w.Flags&flagPushbutton != 0:
		return classText // not label-seeking; class is irrelevant
	case w.Type == "Btn" && (w.Flags&flagRadio != 0 || isCheckboxShaped(w.Box)):
		return classCheck
	case w.Type == "Tx" && w.Flags&flagComb != 0:
		return classComb
	case w.Type == "Sig":
		return classSig
	default:
		return classText
	}
}

// isCheckboxShaped is the fallback for checkboxes, whose /Ff flags are all
// zero: a small square widget is a checkbox, not a text entry.
func isCheckboxShaped(b geom.Box) bool { return b.Width() < 22 && b.Height() < 22 }

// classPriors weight caption directions per widget class. Values follow the
// convergent evidence (Adobe direction table, Chromium's checkable
// inversion, Dragut R6, LabelEx placement stats) and were validated on KS2.
func classPriors(c class) map[direction]float64 {
	switch c {
	case classCheck:
		return map[direction]float64{dirRight: 3, dirLeft: 2, dirAbove: 1, dirBelow: 0.3}
	case classComb, classSig:
		return map[direction]float64{dirBelow: 3, dirLeft: 2.5, dirAbove: 1.5, dirRight: 0.5}
	default:
		return map[direction]float64{dirLeft: 3, dirAbove: 2.5, dirBelow: 0.8, dirRight: 0.5}
	}
}

// candidate is one text cell considered as a unit's caption.
type candidate struct {
	cell  page.TextCell
	index int
	dir   direction
	gap   float64 // edge-to-edge distance along the direction axis
	mis   float64 // misalignment on the cross axis
}

// candidatesFor gathers the plausible caption candidates around box.
func candidatesFor(box geom.Box, cells []page.TextCell, blockers []geom.Box) []candidate {
	var out []candidate
	for i, c := range cells {
		if strings.TrimSpace(c.Text) == "" {
			continue
		}
		dir, gap, mis, ok := classify(box, c.Box)
		if !ok || gap > gapCaps[dir] {
			continue
		}
		if occluded(box, c.Box, dir, blockers) {
			continue
		}
		out = append(out, candidate{cell: c, index: i, dir: dir, gap: gap, mis: mis})
	}
	return out
}

func vOverlap(a, b geom.Box) float64 { return math.Min(a.B, b.B) - math.Max(a.T, b.T) }
func hOverlap(a, b geom.Box) float64 { return math.Min(a.R, b.R) - math.Max(a.L, b.L) }

// classify returns the direction, gap, and cross-axis misalignment of cell
// relative to field, or false when the cell is not a plausible candidate.
// Left/right need real vertical overlap (same band); above/below need
// horizontal overlap, with alignment measured as the best of left-edge,
// centre, and right-edge agreement (captions come in all three styles).
func classify(field, cell geom.Box) (direction, float64, float64, bool) {
	if vOverlap(field, cell) > 0.5*math.Min(field.Height(), cell.Height()) {
		if cell.R <= field.L+1 {
			return dirLeft, field.L - cell.R, math.Abs(cell.CenterY() - field.CenterY()), true
		}
		if cell.L >= field.R-1 {
			return dirRight, cell.L - field.R, math.Abs(cell.CenterY() - field.CenterY()), true
		}
	}
	if hOverlap(field, cell) > 0 {
		mis := math.Min(math.Abs(cell.L-field.L),
			math.Min(math.Abs(cell.CenterX()-field.CenterX()), math.Abs(cell.R-field.R)))
		if cell.B <= field.T+2 {
			return dirAbove, field.T - cell.B, mis, true
		}
		if cell.T >= field.B-2 {
			return dirBelow, cell.T - field.B, mis, true
		}
	}
	return "", 0, 0, false
}

// occluded reports whether any blocker widget lies between the field and the
// candidate cell along dir: a caption never belongs to a field when another
// field sits in between (Chromium stops label harvest at any form control;
// OPAL calls this overshadowing).
func occluded(field, cell geom.Box, dir direction, blockers []geom.Box) bool {
	for _, o := range blockers {
		switch dir {
		case dirLeft:
			if vOverlap(field, o) > 1 && o.R > cell.R && o.L < field.L && o.R <= field.L+1 {
				return true
			}
		case dirRight:
			if vOverlap(field, o) > 1 && o.L < cell.L && o.R > field.R && o.L >= field.R-1 {
				return true
			}
		case dirAbove:
			if hOverlap(field, o) > 1 && o.B > cell.B && o.T < field.T && o.B <= field.T+2 {
				return true
			}
		case dirBelow:
			if hOverlap(field, o) > 1 && o.T < cell.T && o.B > field.B && o.T >= field.B-2 {
				return true
			}
		}
	}
	return false
}

// labelLikeness prefers caption-shaped text: short noun phrases, optionally
// colon-terminated. Sentences (instructions, footnotes) are penalised —
// LabelEx's text-length feature; LITE prunes at 6 words outright.
func labelLikeness(text string) float64 {
	factor := 1.0
	if strings.HasSuffix(text, ":") {
		factor *= 1.5
	}
	if n := len(strings.Fields(text)); n > 6 {
		factor /= 1 + 0.4*float64(n-6)
	}
	return factor
}

// isSemanticState reports whether a checkbox on-state name looks like a
// human-authored option label ("Yes", "Mr") rather than a generated state
// ("A", "1", "On", "Check Box3").
func isSemanticState(state string) bool {
	if len(state) < 2 || strings.EqualFold(state, "On") || strings.EqualFold(state, "Off") {
		return false
	}
	return !strings.Contains(strings.ToLower(state), "check")
}

// questionPattern matches a question-number token like "1." or "10." that
// anchors a group of fields in numbered forms.
var questionPattern = regexp.MustCompile(`^\d{1,2}\.$`)

// questionGroups finds numbered question anchors and collects every widget
// between one anchor and the next into a question group carrying the anchor
// line's text. Pages without numbered anchors yield no groups.
func questionGroups(boxes []geom.Box, lines []page.TextCell) []Group {
	type anchor struct {
		top  float64
		text string
	}
	var anchors []anchor
	for _, c := range lines {
		first, rest, _ := strings.Cut(strings.TrimSpace(c.Text), " ")
		if !questionPattern.MatchString(first) {
			continue
		}
		type part struct {
			l    float64
			text string
		}
		var parts []part
		if rest = strings.TrimSpace(rest); rest != "" {
			parts = append(parts, part{l: c.Box.L, text: rest})
		}
		// The rest of the anchor's visual line: cells to its right whose
		// vertical centre falls within the anchor's band.
		for _, o := range lines {
			if o.Box.L <= c.Box.L || o.Box.CenterY() < c.Box.T || o.Box.CenterY() > c.Box.B {
				continue
			}
			if ot := strings.TrimSpace(o.Text); ot != "" {
				parts = append(parts, part{l: o.Box.L, text: ot})
			}
		}
		if len(parts) == 0 {
			continue
		}
		sort.Slice(parts, func(i, j int) bool { return parts[i].l < parts[j].l })
		texts := make([]string, len(parts))
		for i, p := range parts {
			texts[i] = p.text
		}
		anchors = append(anchors, anchor{top: c.Box.T, text: strings.Join(texts, " ")})
	}
	sort.Slice(anchors, func(i, j int) bool { return anchors[i].top < anchors[j].top })

	members := make([][]int, len(anchors))
	for i, b := range boxes {
		for a := len(anchors) - 1; a >= 0; a-- {
			if anchors[a].top <= b.CenterY() {
				members[a] = append(members[a], i)
				break
			}
		}
	}
	var out []Group
	for a, fields := range members {
		if len(fields) > 0 {
			out = append(out, Group{Kind: GroupQuestion, Label: anchors[a].text, Fields: fields})
		}
	}
	return out
}
