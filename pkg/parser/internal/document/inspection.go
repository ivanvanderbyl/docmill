package document

import (
	"context"
	"fmt"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

// HasActiveContent conservatively detects the EmbeddedFiles and JavaScript
// keys and JavaScript/Launch names anywhere in parsed indirect objects.
// Action names follow PDFium's CPDF_Action::GetType (ISO 32000-1 table 193).
// This matches the upload policy; a false result is not a general safety verdict.
func (d *Document) HasActiveContent(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if d == nil || d.prs == nil {
		return false, fmt.Errorf("inspect PDF: nil document")
	}
	seen := make(map[objects.Object]bool)
	for n := uint32(1); n <= d.GetLastObjNum(); n++ {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		found, err := inspectObjects(ctx, d.GetOrParseIndirectObject(n), seen)
		if err != nil || found {
			return found, err
		}
		if err := d.DecryptionError(); err != nil {
			return false, err
		}
	}
	return false, nil
}

func inspectObjects(ctx context.Context, root objects.Object, seen map[objects.Object]bool) (bool, error) {
	stack := []objects.Object{root}
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		obj := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if obj == nil || seen[obj] {
			continue
		}
		seen[obj] = true
		switch value := obj.(type) {
		case *objects.Name:
			if value.GetString() == "JavaScript" || value.GetString() == "Launch" {
				return true, nil
			}
		case *objects.Reference:
			stack = append(stack, objects.Direct(value))
		case *objects.Stream:
			stack = append(stack, value.Dict())
		case *objects.Dictionary:
			var found bool
			stack, found = inspectDictionary(value, stack)
			if found {
				return true, nil
			}
		case *objects.Array:
			for i := 0; i < value.Len(); i++ {
				stack = append(stack, value.GetObjectAt(i))
			}
		}
	}
	return false, nil
}

func inspectDictionary(value *objects.Dictionary, stack []objects.Object) ([]objects.Object, bool) {
	for _, key := range value.GetKeys() {
		if key == "EmbeddedFiles" || key == "JavaScript" {
			return stack, true
		}
		stack = append(stack, value.GetObjectFor(key))
	}
	return stack, false
}
