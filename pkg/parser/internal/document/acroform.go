package document

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/crt"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

const formFieldMaxRecursion = 32

type FormField struct {
	Name          string
	AlternateName string
	Type          string
	Value         string
	OnState       string
	Flags         int
	Rect          crt.FloatRect
}

// PageFormFields extracts filled AcroForm widget values that appear on pageDict.
// The traversal mirrors PDFium's CPDF_InteractiveForm/CPDF_FormField model:
// widgets are page annotations, field attributes are inherited through /Parent,
// and the full field name is built from parent /T components.
func (d *Document) PageFormFields(pageDict *objects.Dictionary) []FormField {
	if d == nil || pageDict == nil {
		return nil
	}

	annots := pageDict.GetArrayFor("Annots")
	if annots == nil || annots.IsEmpty() {
		return nil
	}

	fields := make([]FormField, 0)
	seenWidgets := make(map[*objects.Dictionary]struct{})
	seenFields := make(map[*objects.Dictionary]struct{})
	for i := 0; i < annots.Len(); i++ {
		widget := annots.GetDictAt(i)
		if widget == nil || widget.GetNameFor("Subtype") != "Widget" {
			continue
		}
		if _, ok := seenWidgets[widget]; ok {
			continue
		}
		seenWidgets[widget] = struct{}{}

		fieldDict := formFieldDictForWidget(widget)
		if fieldDict == nil {
			continue
		}
		if _, ok := seenFields[fieldDict]; ok {
			continue
		}
		seenFields[fieldDict] = struct{}{}

		field := formFieldFromWidget(fieldDict, widget)
		if field.Name == "" || !isFilledFormValue(field.Value) || field.Rect.IsEmpty() {
			continue
		}
		fields = append(fields, field)
	}
	return fields
}

// PageFormFieldWidgets returns one FormField per AcroForm widget annotation on
// pageDict, for layout consumers. Unlike PageFormFields it keeps unfilled
// fields and reports every widget of a multi-widget field (e.g. each radio
// button) rather than collapsing to the first; widgets with no /T name in
// their field chain or an empty /Rect are still skipped.
func (d *Document) PageFormFieldWidgets(pageDict *objects.Dictionary) []FormField {
	if d == nil || pageDict == nil {
		return nil
	}

	annots := pageDict.GetArrayFor("Annots")
	if annots == nil || annots.IsEmpty() {
		return nil
	}

	fields := make([]FormField, 0)
	seenWidgets := make(map[*objects.Dictionary]struct{})
	for i := 0; i < annots.Len(); i++ {
		widget := annots.GetDictAt(i)
		if widget == nil || widget.GetNameFor("Subtype") != "Widget" {
			continue
		}
		if _, ok := seenWidgets[widget]; ok {
			continue
		}
		seenWidgets[widget] = struct{}{}

		fieldDict := formFieldDictForWidget(widget)
		if fieldDict == nil {
			continue
		}
		field := formFieldFromWidget(fieldDict, widget)
		if field.Name == "" || field.Rect.IsEmpty() {
			continue
		}
		fields = append(fields, field)
	}
	return fields
}

// AcroFormFields returns terminal AcroForm fields from the document field tree,
// including blank fields. This is the form-population path; unlike
// PageFormFields it does not filter out empty values or Off buttons.
func (d *Document) AcroFormFields() []FormField {
	dicts := d.acroFormFieldDicts()
	fields := make([]FormField, 0, len(dicts))
	for _, dict := range dicts {
		field := formFieldFromFieldDict(dict)
		if field.Name == "" {
			continue
		}
		fields = append(fields, field)
	}
	return fields
}

// AcroFormFieldValues returns a JSON-friendly field-name/value map for every
// terminal AcroForm field.
func (d *Document) AcroFormFieldValues() map[string]string {
	fields := d.AcroFormFields()
	values := make(map[string]string, len(fields))
	for _, field := range fields {
		values[field.Name] = field.Value
	}
	return values
}

// SetAcroFormFieldValues writes values into matching terminal AcroForm fields.
// It mirrors PDFium's CPDF_FormField::SetValue shape: text/choice values are
// stored as PDF strings in /V; button states are stored as names and "Off"
// clears the state.
func (d *Document) SetAcroFormFieldValues(values map[string]string) (int, error) {
	if len(values) == 0 {
		return 0, nil
	}

	fields := d.acroFormFieldDicts()
	byName := make(map[string]*objects.Dictionary, len(fields))
	for _, field := range fields {
		name := formFieldFullName(field)
		if name != "" {
			byName[name] = field
		}
		if alternateName := strings.TrimSpace(formFieldAttrText(field, "TU")); alternateName != "" {
			byName[alternateName] = field
		}
	}

	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)

	var unknown []string
	changed := 0
	for _, name := range names {
		field := byName[name]
		if field == nil {
			unknown = append(unknown, name)
			continue
		}
		setFormFieldValue(field, values[name])
		changed++
	}
	if len(unknown) > 0 {
		return changed, fmt.Errorf("unknown AcroForm fields: %s", strings.Join(unknown, ", "))
	}
	if changed > 0 {
		if acroForm := d.acroFormDict(); acroForm != nil {
			acroForm.SetFor("NeedAppearances", objects.NewBoolean(true))
		}
	}
	return changed, nil
}

func (d *Document) acroFormFieldDicts() []*objects.Dictionary {
	acroForm := d.acroFormDict()
	if acroForm == nil {
		return nil
	}
	fieldsArray := acroForm.GetArrayFor("Fields")
	if fieldsArray == nil || fieldsArray.IsEmpty() {
		return nil
	}

	var fields []*objects.Dictionary
	seen := make(map[*objects.Dictionary]struct{})
	for i := 0; i < fieldsArray.Len(); i++ {
		d.collectAcroFormFieldDicts(fieldsArray.GetDictAt(i), seen, &fields)
	}
	return fields
}

func (d *Document) collectAcroFormFieldDicts(field *objects.Dictionary, seen map[*objects.Dictionary]struct{}, out *[]*objects.Dictionary) {
	if field == nil {
		return
	}
	if _, ok := seen[field]; ok {
		return
	}
	seen[field] = struct{}{}

	if formFieldAttrString(field, "FT") != "" && formFieldFullName(field) != "" {
		*out = append(*out, field)
	}

	kids := field.GetArrayFor("Kids")
	if kids == nil {
		return
	}
	for i := 0; i < kids.Len(); i++ {
		kid := kids.GetDictAt(i)
		if !isFieldTreeKid(kid) {
			continue
		}
		d.collectAcroFormFieldDicts(kid, seen, out)
	}
}

func (d *Document) acroFormDict() *objects.Dictionary {
	if d == nil || d.rootDict == nil {
		return nil
	}
	return d.rootDict.GetDictFor("AcroForm")
}

func formFieldFromWidget(fieldDict, widget *objects.Dictionary) FormField {
	rect := widget.GetRectFor("Rect")
	rect.Normalize()

	field := formFieldFromFieldDict(fieldDict)
	field.Rect = rect
	field.Value = strings.TrimSpace(field.Value)
	field.OnState = widgetOnState(widget)
	return field
}

// widgetOnState returns the widget's checked appearance-state name: the first
// non-Off key of the /AP /N state dictionary. Checkbox and radio widgets name
// their "on" state after the option they represent (e.g. /Yes, /Mr), which
// makes it a labelling signal that needs no geometry. Push buttons (whose /N
// is a single form XObject stream, not a state dictionary) yield "".
func widgetOnState(widget *objects.Dictionary) string {
	ap := widget.GetDictFor("AP")
	if ap == nil {
		return ""
	}
	states, ok := objects.Direct(ap.GetObjectFor("N")).(*objects.Dictionary)
	if !ok {
		return ""
	}
	for _, key := range states.GetKeys() {
		if !strings.EqualFold(key, "Off") {
			return key
		}
	}
	return ""
}

func formFieldFromFieldDict(fieldDict *objects.Dictionary) FormField {
	return FormField{
		Name:          formFieldFullName(fieldDict),
		AlternateName: strings.TrimSpace(formFieldAttrText(fieldDict, "TU")),
		Type:          strings.TrimSpace(formFieldAttrString(fieldDict, "FT")),
		Value:         formFieldValue(fieldDict),
		Flags:         formFieldAttrInteger(fieldDict, "Ff"),
		Rect:          formFieldRect(fieldDict),
	}
}

func formFieldAttrInteger(fieldDict *objects.Dictionary, name string) int {
	if obj := formFieldAttr(fieldDict, name, 0); obj != nil {
		return obj.GetInteger()
	}
	return 0
}

func formFieldDictForWidget(widget *objects.Dictionary) *objects.Dictionary {
	if widget == nil {
		return nil
	}
	if parent := widget.GetDictFor("Parent"); parent != nil {
		return parent
	}
	return widget
}

func isFieldTreeKid(dict *objects.Dictionary) bool {
	if dict == nil {
		return false
	}
	if dict.GetNameFor("Subtype") != "Widget" {
		return true
	}
	return dict.KeyExist("T") || dict.KeyExist("FT") || dict.KeyExist("Kids")
}

func formFieldFullName(fieldDict *objects.Dictionary) string {
	var parts []string
	visited := make(map[*objects.Dictionary]struct{})
	for level := fieldDict; level != nil; level = level.GetDictFor("Parent") {
		if _, ok := visited[level]; ok {
			break
		}
		visited[level] = struct{}{}
		if shortName := strings.TrimSpace(level.GetUnicodeTextFor("T")); shortName != "" {
			parts = append([]string{shortName}, parts...)
		}
	}
	return strings.Join(parts, ".")
}

func formFieldRect(fieldDict *objects.Dictionary) crt.FloatRect {
	if fieldDict == nil {
		return crt.FloatRect{}
	}
	if fieldDict.GetNameFor("Subtype") == "Widget" {
		rect := fieldDict.GetRectFor("Rect")
		rect.Normalize()
		return rect
	}
	kids := fieldDict.GetArrayFor("Kids")
	if kids == nil {
		return crt.FloatRect{}
	}
	for i := 0; i < kids.Len(); i++ {
		kid := kids.GetDictAt(i)
		if kid == nil || kid.GetNameFor("Subtype") != "Widget" {
			continue
		}
		rect := kid.GetRectFor("Rect")
		rect.Normalize()
		return rect
	}
	return crt.FloatRect{}
}

func formFieldValue(fieldDict *objects.Dictionary) string {
	fieldType := formFieldAttrString(fieldDict, "FT")
	value := formFieldAttr(fieldDict, "V", 0)
	if value == nil && fieldType != "Tx" {
		value = formFieldAttr(fieldDict, "DV", 0)
	}
	return formFieldObjectText(value)
}

func formFieldAttrString(fieldDict *objects.Dictionary, name string) string {
	if obj := formFieldAttr(fieldDict, name, 0); obj != nil {
		return obj.GetString()
	}
	return ""
}

func formFieldAttrText(fieldDict *objects.Dictionary, name string) string {
	return formFieldObjectText(formFieldAttr(fieldDict, name, 0))
}

func formFieldAttr(fieldDict *objects.Dictionary, name string, level int) objects.Object {
	if fieldDict == nil || level > formFieldMaxRecursion {
		return nil
	}
	if obj := fieldDict.GetDirectObjectFor(name); obj != nil {
		return obj
	}
	return formFieldAttr(fieldDict.GetDictFor("Parent"), name, level+1)
}

func formFieldObjectText(obj objects.Object) string {
	obj = objects.Direct(obj)
	switch value := obj.(type) {
	case nil:
		return ""
	case *objects.Array:
		parts := make([]string, 0, value.Len())
		for i := 0; i < value.Len(); i++ {
			if text := strings.TrimSpace(formFieldObjectText(value.GetDirectObjectAt(i))); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ", ")
	default:
		if text := obj.GetUnicodeText(); text != "" {
			return text
		}
		return obj.GetString()
	}
}

func isFilledFormValue(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.EqualFold(value, "Off")
}

func setFormFieldValue(fieldDict *objects.Dictionary, value string) {
	fieldType := formFieldAttrString(fieldDict, "FT")
	switch fieldType {
	case "Btn":
		name := strings.TrimSpace(value)
		if name == "" || strings.EqualFold(name, "false") || strings.EqualFold(name, "off") || name == "0" {
			name = "Off"
		}
		fieldDict.SetFor("V", objects.NewName(name))
		setButtonWidgetState(fieldDict, name)
	default:
		fieldDict.SetFor("V", newPDFTextString(value))
		if fieldType == "Ch" {
			fieldDict.RemoveFor("I")
		}
	}
}

func setButtonWidgetState(fieldDict *objects.Dictionary, name string) {
	if fieldDict.GetNameFor("Subtype") == "Widget" {
		fieldDict.SetNewNameFor("AS", name)
	}
	kids := fieldDict.GetArrayFor("Kids")
	if kids == nil {
		return
	}
	for i := 0; i < kids.Len(); i++ {
		kid := kids.GetDictAt(i)
		if kid == nil || kid.GetNameFor("Subtype") != "Widget" {
			continue
		}
		kid.SetNewNameFor("AS", name)
	}
}

func newPDFTextString(value string) *objects.String {
	if isPDFDocASCII(value) {
		return objects.NewString(value, false)
	}
	runes := utf16.Encode([]rune(value))
	buf := make([]byte, 2, 2+len(runes)*2)
	buf[0] = 0xfe
	buf[1] = 0xff
	for _, r := range runes {
		buf = append(buf, byte(r>>8), byte(r))
	}
	return objects.NewString(string(buf), false)
}

func isPDFDocASCII(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] >= 0x80 {
			return false
		}
	}
	return true
}
