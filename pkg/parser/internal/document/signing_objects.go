package document

import (
	"fmt"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

// Incremental revisions retain old object generations. The full-rewrite
// serializer deliberately normalizes them and cannot be used for these refs.
func (r *signingRevision) writeReference(number uint32) error {
	generation := r.generations[number]
	if _, ok := r.records[number]; !ok && number != r.signature {
		obj := r.source.GetOrParseIndirectObject(number)
		if obj == nil {
			return fmt.Errorf("prepare signature: unresolved object %d", number)
		}
		generation = obj.GetGenNum()
	}
	fmt.Fprintf(&r.data, "%d %d R", number, generation)
	return nil
}

func (r *signingRevision) writeObject(obj objects.Object, depth int, visited map[objects.Object]bool) error {
	if obj == nil {
		r.data.WriteString("null")
		return nil
	}
	if depth > 128 {
		return fmt.Errorf("prepare signature: direct object depth limit")
	}
	if ref := objects.ToReference(obj); ref != nil {
		return r.writeReference(ref.GetRefObjNum())
	}
	if !obj.IsInline() {
		return r.writeReference(obj.GetObjNum())
	}
	if visited[obj] {
		return fmt.Errorf("prepare signature: cyclic direct object")
	}
	visited[obj] = true
	defer delete(visited, obj)
	switch value := obj.(type) {
	case *objects.Array:
		return r.writeArray(value, depth, visited)
	case *objects.Dictionary:
		r.data.WriteString("<<")
		if err := r.writeDictionaryEntries(value, depth, visited); err != nil {
			return err
		}
		r.data.WriteString(" >>")
	case *objects.String:
		writePDFString(&r.data, value.GetString())
	case *objects.Name:
		r.data.WriteByte('/')
		writePDFName(&r.data, value.GetString())
	case *objects.Number:
		r.data.WriteString(value.SourceLexeme())
	case *objects.Boolean:
		r.data.WriteString(value.GetString())
	case *objects.Null:
		r.data.WriteString("null")
	default:
		return fmt.Errorf("prepare signature: unsupported direct object type %d", obj.Type())
	}
	return nil
}

func (r *signingRevision) writeArray(array *objects.Array, depth int, visited map[objects.Object]bool) error {
	r.data.WriteByte('[')
	for i := 0; i < array.Len(); i++ {
		if i > 0 {
			r.data.WriteByte(' ')
		}
		if err := r.writeObject(array.GetObjectAt(i), depth+1, visited); err != nil {
			return err
		}
	}
	r.data.WriteByte(']')
	return nil
}

func (r *signingRevision) writeDictionaryEntries(dict *objects.Dictionary, depth int, visited map[objects.Object]bool) error {
	for _, key := range dict.GetKeys() {
		r.data.WriteString(" /")
		writePDFName(&r.data, key)
		r.data.WriteByte(' ')
		if err := r.writeObject(dict.GetObjectFor(key), depth+1, visited); err != nil {
			return err
		}
	}
	return nil
}
