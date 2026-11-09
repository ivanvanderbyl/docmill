package document

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/parser"
)

func (d *Document) signingSource(ctx context.Context) (*Document, []byte, error) {
	if d.IsEncrypted() {
		return nil, nil, fmt.Errorf("prepare signature: encrypted documents require explicit decryption first")
	}
	signatures, err := d.Signatures()
	if err != nil {
		return nil, nil, err
	}
	if err := d.checkSigningPolicy(signatures); err != nil {
		return nil, nil, err
	}
	var source []byte
	if len(signatures) > 0 {
		if d.HasUnsavedChanges() {
			return nil, nil, fmt.Errorf("prepare signature: signed document has unsaved changes")
		}
		if d.prs.XRefTableRebuilt() {
			return nil, nil, fmt.Errorf("prepare signature: signed document xref was rebuilt")
		}
		if err := d.checkInlineSignatureContents(signatures); err != nil {
			return nil, nil, err
		}
		source = d.SourceBytes()
		if err := validatePriorSignatureRanges(source, signatures); err != nil {
			return nil, nil, err
		}
	} else if !d.HasUnsavedChanges() && !d.prs.XRefTableRebuilt() {
		source = d.SourceBytes()
	} else {
		var snapshot bytes.Buffer
		if err := d.WritePDF(&snapshot); err != nil {
			return nil, nil, err
		}
		source = snapshot.Bytes()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	base, status := Open(source)
	if status != parser.Success {
		return nil, nil, fmt.Errorf("prepare signature: could not open source revision")
	}
	return base, source, nil
}

func (d *Document) checkSigningPolicy(signatures []Signature) error {
	if err := d.checkCatalogSigningPolicy(); err != nil {
		return err
	}
	for _, signature := range signatures {
		if signature.DocMDPPermission > 0 {
			return fmt.Errorf("prepare signature: certification policy is unsupported")
		}
	}
	fields, err := d.signatureFields()
	if err != nil {
		return err
	}
	for _, field := range fields {
		if err := checkFieldSigningPolicy(field); err != nil {
			return err
		}
	}
	return nil
}

func (d *Document) checkCatalogSigningPolicy() error {
	value := d.rootDict.GetDirectObjectFor("Perms")
	if value == nil || objects.ToNull(value) != nil {
		return nil
	}
	permissions := objects.ToDictionary(value)
	if permissions == nil {
		return fmt.Errorf("prepare signature: invalid permissions dictionary")
	}
	for _, key := range []string{"DocMDP", "UR", "UR3"} {
		value := permissions.GetDirectObjectFor(key)
		if value != nil && objects.ToNull(value) == nil {
			return fmt.Errorf("prepare signature: %s policy is unsupported", key)
		}
	}
	return nil
}

func checkFieldSigningPolicy(field *objects.Dictionary) error {
	value, _, err := signatureFieldValue(field)
	if err != nil {
		return err
	}
	if value == nil {
		return nil
	}
	if field.KeyExist("Lock") {
		return fmt.Errorf("prepare signature: field locking policy is unsupported")
	}
	refs := value.GetDirectObjectFor("Reference")
	if refs == nil || objects.ToNull(refs) != nil {
		return nil
	}
	array := objects.ToArray(refs)
	if array == nil || array.Len() > 0 {
		return fmt.Errorf("prepare signature: signature transform policy is unsupported")
	}
	return nil
}

func validatePriorSignatureRanges(source []byte, signatures []Signature) error {
	size := int64(len(source))
	for _, signature := range signatures {
		r := signature.ByteRange
		if len(r) != 4 {
			return fmt.Errorf("prepare signature: invalid existing ByteRange")
		}
		if r[0] != 0 || r[1] < 0 || r[2] < r[1] || r[2] > size || r[3] < 0 || r[3] > size-r[2] {
			return fmt.Errorf("prepare signature: invalid existing ByteRange bounds")
		}
		if r[1] != signature.ContentsStart || r[2] != signature.ContentsEnd || r[2]-r[1] < 2 {
			return fmt.Errorf("prepare signature: existing ByteRange does not delimit Contents")
		}
		if !bytes.HasSuffix(bytes.TrimSpace(source[:r[2]+r[3]]), []byte("%%EOF")) {
			return fmt.Errorf("prepare signature: existing signed revision has no EOF")
		}
	}
	return nil
}

func (d *Document) normalizeSigningOptions(opts SigningOptions) (SigningOptions, error) {
	if opts.ContentsSize == 0 {
		opts.ContentsSize = 32 * 1024
	}
	if opts.ContentsSize < 1 || opts.ContentsSize > 1024*1024 {
		return opts, fmt.Errorf("prepare signature: ContentsSize must be 1 to 1048576 bytes")
	}
	if !utf8.ValidString(opts.FieldName) || strings.Contains(opts.FieldName, ".") || len(opts.FieldName) > 4096 || strings.TrimSpace(opts.FieldName) != opts.FieldName {
		return opts, fmt.Errorf("prepare signature: field name must be one UTF-8 name without periods")
	}
	names, err := d.signingFieldNames()
	if err != nil {
		return opts, err
	}
	if opts.FieldName == "" {
		for i := 1; ; i++ {
			name := "Signature" + strconv.Itoa(i)
			if !names[name] {
				opts.FieldName = name
				break
			}
		}
	}
	if names[opts.FieldName] {
		return opts, fmt.Errorf("prepare signature: field %q already exists", opts.FieldName)
	}
	return opts, nil
}

func signingPreviousXref(source []byte) (int64, error) {
	offset := bytes.LastIndex(source, []byte("startxref"))
	if offset < 0 {
		return 0, fmt.Errorf("prepare signature: source startxref missing")
	}
	tail := bytes.Fields(source[offset+len("startxref"):])
	if len(tail) == 0 {
		return 0, fmt.Errorf("prepare signature: source xref offset missing")
	}
	previous, err := strconv.ParseInt(string(tail[0]), 10, 64)
	if err != nil || previous < 9 || previous >= int64(len(source)) {
		return 0, fmt.Errorf("prepare signature: invalid source xref offset")
	}
	return previous, nil
}

func (d *Document) signingFieldNames() (map[string]bool, error) {
	fields, err := d.signatureFields()
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(fields))
	for _, field := range fields {
		chain, err := signatureFieldParents(field)
		if err != nil {
			return nil, err
		}
		var parts []string
		for _, c := range slices.Backward(chain) {
			if part := strings.TrimSpace(c.GetUnicodeTextFor("T")); part != "" {
				parts = append(parts, part)
			}
		}
		names[strings.Join(parts, ".")] = true
	}
	return names, nil
}

func (d *Document) checkInlineSignatureContents(signatures []Signature) error {
	starts := make(map[int64]bool, len(signatures))
	for _, signature := range signatures {
		starts[signature.ContentsStart] = true
	}
	form := d.acroFormDict()
	if form == nil {
		return nil
	}
	fields := form.GetArrayFor("Fields")
	if fields == nil {
		return nil
	}
	for i := 0; i < fields.Len(); i++ {
		found, err := inlineSignatureContents(fields.GetObjectAt(i), starts, 0, map[objects.Object]bool{})
		if err != nil {
			return err
		}
		if found {
			return fmt.Errorf("prepare signature: existing inline signature Contents cannot be relocated")
		}
	}
	return nil
}

func inlineSignatureContents(obj objects.Object, starts map[int64]bool, depth int, seen map[objects.Object]bool) (bool, error) {
	if obj == nil || !obj.IsInline() || objects.ToReference(obj) != nil {
		return false, nil
	}
	if depth > 64 {
		return false, fmt.Errorf("prepare signature: inline field depth limit")
	}
	if seen[obj] {
		return false, fmt.Errorf("prepare signature: cyclic inline field")
	}
	seen[obj] = true
	defer delete(seen, obj)
	if text := objects.ToString(obj); text != nil {
		start, _, ok := text.SourceRange()
		return ok && starts[start], nil
	}
	var children []objects.Object
	switch value := obj.(type) {
	case *objects.Dictionary:
		for _, key := range value.GetKeys() {
			children = append(children, value.GetObjectFor(key))
		}
	case *objects.Array:
		for i := 0; i < value.Len(); i++ {
			children = append(children, value.GetObjectAt(i))
		}
	}
	for _, child := range children {
		found, err := inlineSignatureContents(child, starts, depth+1, seen)
		if err != nil || found {
			return found, err
		}
	}
	return false, nil
}
