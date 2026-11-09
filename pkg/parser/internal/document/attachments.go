package document

import "github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"

// Attachment describes a file specification in the EmbeddedFiles name tree.
// Names are metadata, not trusted filesystem paths.
type Attachment struct {
	Name        string
	Description string
}

// Attachments enumerates the EmbeddedFiles name tree in document order.
// PDFium references: FPDFDoc_GetAttachment and CPDF_FileSpec::GetFileName.
func (d *Document) Attachments() []Attachment {
	if d == nil || d.rootDict == nil {
		return nil
	}
	names := d.rootDict.GetDictFor("Names")
	if names == nil {
		return nil
	}
	root := names.GetDictFor("EmbeddedFiles")
	var out []Attachment
	stack := []*objects.Dictionary{root}
	seen := make(map[*objects.Dictionary]bool)
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if node == nil || seen[node] {
			continue
		}
		seen[node] = true
		if entries := node.GetArrayFor("Names"); entries != nil {
			for i := 1; i < entries.Len(); i += 2 {
				if a, ok := attachmentSpec(entries.GetDirectObjectAt(i)); ok {
					out = append(out, a)
				}
			}
			continue
		}
		if kids := node.GetArrayFor("Kids"); kids != nil {
			for i := kids.Len() - 1; i >= 0; i-- {
				stack = append(stack, kids.GetDictAt(i))
			}
		}
	}
	return out
}

func attachmentSpec(obj objects.Object) (Attachment, bool) {
	if str := objects.ToString(obj); str != nil {
		return Attachment{Name: str.GetUnicodeText()}, true
	}
	dict := objects.ToDictionary(obj)
	if dict == nil {
		return Attachment{}, false
	}
	a := Attachment{Description: dict.GetUnicodeTextFor("Desc")}
	for _, key := range []string{"UF", "F"} {
		if str := objects.ToString(dict.GetDirectObjectFor(key)); str != nil {
			a.Name = str.GetUnicodeText()
		}
		if a.Name != "" {
			return a, true
		}
	}
	if dict.GetByteStringFor("FS") == "URL" {
		return a, true
	}
	for _, key := range []string{"DOS", "Mac", "Unix"} {
		if str := objects.ToString(dict.GetDirectObjectFor(key)); str != nil {
			a.Name = str.GetUnicodeText()
			break
		}
	}
	return a, true
}
