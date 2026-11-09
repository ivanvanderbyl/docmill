package document

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

// Signature is claimed metadata, not a cryptographic verification result.
// Contents preserves padding; ByteRange is not verified against the source.
type Signature struct {
	FieldName string
	Name      string
	Reason    string
	Location  string
	SubFilter string
	Contents  []byte
	ByteRange []int64
	// SigningTime is nil when /M is absent, malformed, or has no explicit zone.
	SigningTime      *time.Time
	SigningTimeRaw   string
	DocMDPPermission int
	// ContentsStart and ContentsEnd delimit the exact original string token.
	// Both are -1 when the string has no direct original-file representation.
	ContentsStart int64
	ContentsEnd   int64
}

// SourceBytes returns a copy of the exact original input, unaffected by edits.
func (d *Document) SourceBytes() []byte {
	if d == nil {
		return nil
	}
	return append([]byte(nil), d.source...)
}

// Signatures inspects signature fields, certification, and usage-rights signatures.
// Unsigned fields are omitted. This does not authenticate any signature.
// Reference: fpdfsdk/fpdf_signature.cpp blob 915e3a57070c61a37b87dc01284cc3e51245b338.
func (d *Document) Signatures() ([]Signature, error) {
	if d == nil || d.rootDict == nil {
		return nil, fmt.Errorf("signatures: nil document")
	}
	fields, err := d.signatureFields()
	if err != nil {
		return nil, err
	}
	var result []Signature
	seen := map[*objects.Dictionary]bool{}
	for _, field := range fields {
		dict, name, e := signatureFieldValue(field)
		if e != nil {
			return nil, e
		}
		if dict == nil || seen[dict] {
			continue
		}
		seen[dict] = true
		sig, e := readSignature(dict)
		if e != nil {
			return nil, e
		}
		sig.FieldName = name
		result = append(result, sig)
	}
	if perms := d.rootDict.GetDictFor("Perms"); perms != nil {
		for _, key := range []string{"DocMDP", "UR", "UR3"} {
			value := perms.GetDirectObjectFor(key)
			if value == nil || objects.ToNull(value) != nil {
				continue
			}
			dict := objects.ToDictionary(value)
			if dict == nil {
				return nil, fmt.Errorf("signatures: invalid %s signature dictionary", key)
			}
			if seen[dict] {
				continue
			}
			seen[dict] = true
			sig, e := readSignature(dict)
			if e != nil {
				return nil, e
			}
			result = append(result, sig)
		}
	}
	return result, d.DecryptionError()
}

const signatureFieldMaxDepth = 64

func signatureFieldValue(field *objects.Dictionary) (*objects.Dictionary, string, error) {
	chain, err := signatureFieldParents(field)
	if err != nil {
		return nil, "", err
	}
	ft := objects.ToName(signatureFieldAttribute(chain, "FT"))
	if ft == nil || ft.GetString() != "Sig" {
		return nil, "", nil
	}
	value := signatureFieldAttribute(chain, "V")
	if value == nil || objects.ToNull(value) != nil {
		return nil, "", nil
	}
	dict := objects.ToDictionary(value)
	if dict == nil {
		return nil, "", fmt.Errorf("signature: invalid value dictionary")
	}
	var parts []string
	for _, c := range slices.Backward(chain) {
		if name := strings.TrimSpace(c.GetUnicodeTextFor("T")); name != "" {
			parts = append(parts, name)
		}
	}
	return dict, strings.Join(parts, "."), nil
}

func signatureFieldParents(field *objects.Dictionary) ([]*objects.Dictionary, error) {
	var chain []*objects.Dictionary
	seen := make(map[*objects.Dictionary]bool)
	for field != nil {
		if seen[field] {
			return nil, fmt.Errorf("signatures: cyclic field parent")
		}
		if len(chain) > signatureFieldMaxDepth {
			return nil, fmt.Errorf("signatures: field parent nesting exceeds %d", signatureFieldMaxDepth)
		}
		seen[field] = true
		chain = append(chain, field)
		parent := field.GetDirectObjectFor("Parent")
		if parent == nil || objects.ToNull(parent) != nil {
			break
		}
		field = objects.ToDictionary(parent)
		if field == nil {
			return nil, fmt.Errorf("signatures: invalid field parent dictionary")
		}
	}
	return chain, nil
}

func signatureFieldAttribute(chain []*objects.Dictionary, key string) objects.Object {
	for _, field := range chain {
		if value := field.GetDirectObjectFor(key); value != nil {
			return value
		}
	}
	return nil
}

func (d *Document) signatureFields() ([]*objects.Dictionary, error) {
	form := d.acroFormDict()
	if form == nil {
		return nil, nil
	}
	fields := form.GetArrayFor("Fields")
	if fields == nil {
		if form.KeyExist("Fields") {
			return nil, fmt.Errorf("signatures: invalid field array")
		}
		return nil, nil
	}
	type entry struct {
		field *objects.Dictionary
		depth int
	}
	var stack []entry
	for i := fields.Len() - 1; i >= 0; i-- {
		stack = append(stack, entry{fields.GetDictAt(i), 0})
	}
	var out []*objects.Dictionary
	seen := map[*objects.Dictionary]bool{}
	for len(stack) > 0 {
		next := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if next.field == nil || seen[next.field] {
			continue
		}
		if next.depth > signatureFieldMaxDepth {
			return nil, fmt.Errorf("signatures: field nesting exceeds %d", signatureFieldMaxDepth)
		}
		seen[next.field] = true
		out = append(out, next.field)
		if kids := next.field.GetArrayFor("Kids"); kids != nil {
			for i := kids.Len() - 1; i >= 0; i-- {
				stack = append(stack, entry{kids.GetDictAt(i), next.depth + 1})
			}
		}
	}
	return out, nil
}

func readSignature(dict *objects.Dictionary) (Signature, error) {
	sig := Signature{ContentsStart: -1, ContentsEnd: -1}
	for _, metadata := range []struct {
		key  string
		dest *string
	}{{"Name", &sig.Name}, {"Reason", &sig.Reason}, {"Location", &sig.Location}, {"M", &sig.SigningTimeRaw}} {
		key, dest := metadata.key, metadata.dest
		value := dict.GetDirectObjectFor(key)
		if value == nil {
			continue
		}
		s := objects.ToString(value)
		if s == nil {
			return sig, fmt.Errorf("signature: invalid %s string", key)
		}
		*dest = s.GetUnicodeText()
	}
	if value := dict.GetDirectObjectFor("SubFilter"); value != nil {
		n := objects.ToName(value)
		if n == nil {
			return sig, fmt.Errorf("signature: invalid SubFilter name")
		}
		sig.SubFilter = n.GetString()
	}
	if value := dict.GetDirectObjectFor("Contents"); value != nil {
		s := objects.ToString(value)
		if s == nil {
			return sig, fmt.Errorf("signature: invalid Contents string")
		}
		sig.Contents = []byte(s.GetString())
		if start, end, ok := s.SourceRange(); ok {
			sig.ContentsStart = start
			sig.ContentsEnd = end
		}
	}
	if value := dict.GetDirectObjectFor("ByteRange"); value != nil {
		a := objects.ToArray(value)
		if a == nil {
			return sig, fmt.Errorf("signature: invalid ByteRange array")
		}
		sig.ByteRange = make([]int64, a.Len())
		for i := 0; i < a.Len(); i++ {
			n := objects.ToNumber(a.GetDirectObjectAt(i))
			if n == nil {
				return sig, fmt.Errorf("signature: invalid ByteRange integer")
			}
			v, e := strconv.ParseInt(n.SourceLexeme(), 10, 64)
			if e != nil {
				return sig, fmt.Errorf("signature: invalid ByteRange integer: %w", e)
			}
			sig.ByteRange[i] = v
		}
	}
	sig.SigningTime = signatureTime(sig.SigningTimeRaw)
	sig.DocMDPPermission = signaturePermission(dict)
	return sig, nil
}

func signaturePermission(dict *objects.Dictionary) int {
	references := dict.GetArrayFor("Reference")
	if references == nil {
		return 0
	}
	for i := 0; i < references.Len(); i++ {
		ref := references.GetDictAt(i)
		if ref == nil || ref.GetNameFor("TransformMethod") != "DocMDP" {
			continue
		}
		params := ref.GetDictFor("TransformParams")
		if params == nil {
			continue
		}
		permission := params.GetIntegerWithDefaultFor("P", 2)
		if permission < 1 || permission > 3 {
			return 0
		}
		return permission
	}
	return 0
}
func signatureTime(raw string) *time.Time {
	raw = strings.TrimPrefix(raw, "D:")
	if len(raw) < 15 {
		return nil
	}
	date, zone := raw[:14], strings.ReplaceAll(raw[14:], "'", "")
	layout := "20060102150405-0700"
	if zone == "Z" {
		layout = "20060102150405Z"
	} else if len(zone) != 5 {
		return nil
	}
	parsed, err := time.Parse(layout, date+zone)
	if err != nil {
		return nil
	}
	return &parsed
}
