package document

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"slices"
	"strconv"
	"unicode/utf16"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

const signingRangeWidth = 20

type signingRevision struct {
	source      *Document
	records     map[uint32]objects.Object
	generations map[uint32]uint32
	signature   uint32
	last        uint32
	data        bytes.Buffer
	rangeStart  int
	gapStart    int
	gapEnd      int
}

// Append-only xref sections and /Prev follow ISO 32000-1 section 7.5.6 and
// PDFium 6688c576fd851f4159c6fae39396da50939de110, cpdf_creator.cpp.
func (d *Document) appendSignature(ctx context.Context, source []byte, previous int64, opts SigningOptions) (*PreparedSignature, error) {
	revision, root, err := d.signatureRevision(opts)
	if err != nil {
		return nil, err
	}
	revision.data.Write(source)
	revision.data.WriteByte('\n')
	numbers := make([]uint32, 0, len(revision.records))
	for number := range revision.records {
		numbers = append(numbers, number)
	}
	slices.Sort(numbers)
	offsets := make(map[uint32]int64, len(numbers))
	for _, number := range numbers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		offsets[number] = int64(revision.data.Len())
		fmt.Fprintf(&revision.data, "%d %d obj\n", number, revision.generations[number])
		if number == revision.signature {
			err = revision.writeSignature(opts)
		} else {
			err = revision.writeObject(revision.records[number], 0, map[objects.Object]bool{})
		}
		if err != nil {
			return nil, err
		}
		revision.data.WriteString("\nendobj\n")
	}
	xref := revision.data.Len()
	if err := revision.writeXref(numbers, offsets); err != nil {
		return nil, err
	}
	trailer := objects.NewDictionary()
	trailer.SetFor("Size", objects.NewNumberFromInt(int32(revision.last+1)))
	trailer.SetFor("Root", objects.NewReference(d, root))
	trailer.SetFor("Prev", objects.NewNumberFromString(fmt.Sprint(previous)))
	for _, key := range []string{"Info", "ID"} {
		if value := d.prs.GetTrailer().GetObjectFor(key); value != nil {
			trailer.SetFor(key, value)
		}
	}
	revision.data.WriteString("trailer\n")
	if err := revision.writeObject(trailer, 0, map[objects.Object]bool{}); err != nil {
		return nil, err
	}
	fmt.Fprintf(&revision.data, "\nstartxref\n%d\n%%%%EOF\n", xref)
	ranges := [4]int64{0, int64(revision.gapStart), int64(revision.gapEnd), int64(revision.data.Len() - revision.gapEnd)}
	var rangeText bytes.Buffer
	for i, value := range ranges {
		if i > 0 {
			rangeText.WriteByte(' ')
		}
		fmt.Fprintf(&rangeText, "%020d", value)
	}
	result := bytes.Clone(revision.data.Bytes())
	copy(result[revision.rangeStart:revision.rangeStart+rangeText.Len()], rangeText.Bytes())
	return &PreparedSignature{
		FieldName:     opts.FieldName,
		ByteRange:     ranges,
		ContentsStart: ranges[1],
		ContentsEnd:   ranges[2],
		data:          result,
		gapStart:      revision.gapStart,
		gapEnd:        revision.gapEnd,
	}, nil
}

func (d *Document) signatureRevision(opts SigningOptions) (*signingRevision, uint32, error) {
	last := max(d.prs.GetLastObjNum(), d.GetLastObjNum())
	if last > 4*1024*1024-6 {
		return nil, 0, fmt.Errorf("prepare signature: object number limit reached")
	}
	r := &signingRevision{
		source:      d,
		records:     make(map[uint32]objects.Object),
		generations: make(map[uint32]uint32),
		signature:   last + 1,
		last:        last + 5,
	}
	r.records[r.signature] = objects.NewDictionary()
	page := d.GetPageDict(0)
	if page == nil || page.GetObjNum() == 0 {
		return nil, 0, fmt.Errorf("prepare signature: first page must be indirect")
	}
	r.generations[page.GetObjNum()] = page.GetGenNum()
	field := objects.NewDictionary()
	field.SetNewNameFor("Type", "Annot")
	field.SetNewNameFor("Subtype", "Widget")
	rectangle := objects.NewArray()
	for range 4 {
		rectangle.Append(objects.NewNumberFromInt(0))
	}
	field.SetFor("Rect", rectangle)
	field.SetFor("P", objects.NewReference(d, page.GetObjNum()))
	field.SetNewNameFor("FT", "Sig")
	field.SetFor("T", signingText(opts.FieldName))
	field.SetFor("V", objects.NewReference(d, last+1))
	r.records[last+2] = field
	if err := r.addPageWidget(page, last+2); err != nil {
		return nil, 0, err
	}
	form := d.acroFormDict()
	if form == nil {
		if value := d.rootDict.GetDirectObjectFor("AcroForm"); value != nil && objects.ToNull(value) == nil {
			return nil, 0, fmt.Errorf("prepare signature: invalid AcroForm")
		}
		form = objects.NewDictionary()
	} else {
		form = signingDictionaryCopy(form)
	}
	fields := objects.NewArray()
	if existing := form.GetDirectObjectFor("Fields"); existing != nil && objects.ToNull(existing) == nil {
		old := objects.ToArray(existing)
		if old == nil {
			return nil, 0, fmt.Errorf("prepare signature: invalid Fields array")
		}
		for i := 0; i < old.Len(); i++ {
			fields.Append(old.GetObjectAt(i))
		}
	}
	fields.Append(objects.NewReference(d, last+2))
	r.records[last+3] = fields
	form.SetFor("Fields", objects.NewReference(d, last+3))
	form.SetNewNumberFor("SigFlags", int32(form.GetIntegerFor("SigFlags")|3))
	r.records[last+4] = form
	catalog := signingDictionaryCopy(d.rootDict)
	catalog.SetFor("AcroForm", objects.NewReference(d, last+4))
	if d.prs.FileVersion() < 17 && signingVersion(catalog.GetNameFor("Version")) < 17 {
		catalog.SetNewNameFor("Version", "1.7")
	}
	r.records[last+5] = catalog
	return r, last + 5, nil
}

func signingText(value string) *objects.String {
	units := utf16.Encode([]rune(value))
	data := make([]byte, 2+len(units)*2)
	data[0] = 0xfe
	data[1] = 0xff
	for i, unit := range units {
		binary.BigEndian.PutUint16(data[2+i*2:], unit)
	}
	return objects.NewString(string(data), false)
}

func (r *signingRevision) writeSignature(opts SigningOptions) error {
	dict := objects.NewDictionary()
	dict.SetNewNameFor("Type", "Sig")
	dict.SetNewNameFor("Filter", "Adobe.PPKLite")
	dict.SetNewNameFor("SubFilter", "adbe.pkcs7.detached")
	for _, entry := range []struct{ key, value string }{{"Name", opts.Name}, {"Reason", opts.Reason}, {"Location", opts.Location}} {
		if entry.value != "" {
			dict.SetFor(entry.key, signingText(entry.value))
		}
	}
	if !opts.SigningTime.IsZero() {
		dict.SetFor("M", objects.NewString(opts.SigningTime.UTC().Format("D:20060102150405Z"), false))
	}
	r.data.WriteString("<<")
	if err := r.writeDictionaryEntries(dict, 0, map[objects.Object]bool{}); err != nil {
		return err
	}
	r.data.WriteString(" /ByteRange [")
	r.rangeStart = r.data.Len()
	for i := range 4 {
		if i > 0 {
			r.data.WriteByte(' ')
		}
		r.data.Write(bytes.Repeat([]byte{'0'}, signingRangeWidth))
	}
	r.data.WriteString("] /Contents ")
	r.gapStart = r.data.Len()
	r.data.WriteByte('<')
	r.data.Write(bytes.Repeat([]byte{'0'}, opts.ContentsSize*2))
	r.data.WriteByte('>')
	r.gapEnd = r.data.Len()
	r.data.WriteString(" >>")
	return nil
}

func signingDictionaryCopy(source *objects.Dictionary) *objects.Dictionary {
	copy := objects.NewDictionary()
	for _, key := range source.GetKeys() {
		copy.SetFor(key, source.GetObjectFor(key))
	}
	return copy
}

func (r *signingRevision) writeXref(numbers []uint32, offsets map[uint32]int64) error {
	r.data.WriteString("xref\n")
	for start := 0; start < len(numbers); {
		end := start + 1
		for end < len(numbers) && numbers[end] == numbers[end-1]+1 {
			end++
		}
		fmt.Fprintf(&r.data, "%d %d\n", numbers[start], end-start)
		for _, number := range numbers[start:end] {
			offset := offsets[number]
			if offset >= 10_000_000_000 {
				return fmt.Errorf("prepare signature: xref offset exceeds classic table capacity")
			}
			fmt.Fprintf(&r.data, "%010d %05d n \n", offset, r.generations[number])
		}
		start = end
	}
	return nil
}

func (r *signingRevision) addPageWidget(page *objects.Dictionary, field uint32) error {
	updated := signingDictionaryCopy(page)
	annots := objects.NewArray()
	if existing := page.GetDirectObjectFor("Annots"); existing != nil && objects.ToNull(existing) == nil {
		old := objects.ToArray(existing)
		if old == nil {
			return fmt.Errorf("prepare signature: invalid page annotations")
		}
		for i := 0; i < old.Len(); i++ {
			annots.Append(old.GetObjectAt(i))
		}
	}
	annots.Append(objects.NewReference(r.source, field))
	updated.SetFor("Annots", annots)
	r.records[page.GetObjNum()] = updated
	return nil
}

func signingVersion(version string) int {
	if len(version) != 3 || version[1] != '.' {
		return 0
	}
	major, err := strconv.Atoi(version[:1])
	if err != nil {
		return 0
	}
	minor, err := strconv.Atoi(version[2:])
	if err != nil {
		return 0
	}
	return major*10 + minor
}
