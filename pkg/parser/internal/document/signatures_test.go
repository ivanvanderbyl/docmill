package document

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/parser"
	"github.com/stretchr/testify/require"
)

func TestSignatureInspectionFieldsAndCertification(t *testing.T) {
	data := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 6 0 R 7 0 R] >> /Perms << /DocMDP 5 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R >>",
		"<< /FT /Sig /T (approval) /V 5 0 R >>",
		"<< /Type /Sig /Name (Alice) /Reason (Approved) /Location (Melbourne) /SubFilter /adbe.pkcs7.detached /Contents <3001020000> /ByteRange [0 12 30 40] /M (D:20261109123045+11'00') /Reference [<< /TransformMethod /DocMDP /TransformParams << /P 1 >> >>] >>",
		"<< /FT /Sig /T (unsigned) >>",
		"<< /T (parent) /Kids [8 0 R] >>",
		"<< /FT /Sig /T (child) /Parent 7 0 R /V 9 0 R >>",
		"<< /Type /Sig /Contents <abcd> /ByteRange [0 9007199254740993 9007199254740994 2] >>",
	}, "1 0 R")
	d, e := Open(data)
	require.Equal(t, parser.Success, e)
	got, err := d.Signatures()
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "approval", got[0].FieldName)
	require.Equal(t, "Alice", got[0].Name)
	require.Equal(t, "Approved", got[0].Reason)
	require.Equal(t, "Melbourne", got[0].Location)
	require.Equal(t, "adbe.pkcs7.detached", got[0].SubFilter)
	require.Equal(t, []byte{0x30, 1, 2, 0, 0}, got[0].Contents)
	require.Equal(t, []int64{0, 12, 30, 40}, got[0].ByteRange)
	require.Equal(t, 1, got[0].DocMDPPermission)
	require.NotNil(t, got[0].SigningTime)
	require.Equal(t, time.Date(2026, 11, 9, 1, 30, 45, 0, time.UTC), got[0].SigningTime.UTC())
	require.Equal(t, "parent.child", got[1].FieldName)
	require.Equal(t, []int64{0, 9007199254740993, 9007199254740994, 2}, got[1].ByteRange)
	got[0].Contents[0] = 0
	got[0].ByteRange[0] = 99
	again, err := d.Signatures()
	require.NoError(t, err)
	require.Equal(t, byte(0x30), again[0].Contents[0])
	require.Zero(t, again[0].ByteRange[0])
}

func TestSignatureInspectionMalformedAndCyclicFields(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		wantErr     bool
	}{
		{"unsigned null", "null", false}, {"wrong value", "42", true}, {"wrong contents", "<< /Contents [] >>", true}, {"wrong byte range", "<< /ByteRange [(no)] >>", true}, {"fractional byte range", "<< /ByteRange [0 1.5] >>", true}, {"empty signature", "<< >>", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := buildPDF([]string{"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R >>", "<< /FT /Sig /T (loop) /Kids [4 0 R] /V " + tc.value + " >>"}, "1 0 R")
			d, e := Open(data)
			require.Equal(t, parser.Success, e)
			got, err := d.Signatures()
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.value == "null" {
				require.Empty(t, got)
			} else {
				require.Len(t, got, 1)
			}
		})
	}
}

func TestCertificationWithoutSignatureField(t *testing.T) {
	d, e := Open(buildPDF([]string{"<< /Type /Catalog /Pages 2 0 R /Perms << /DocMDP 4 0 R >> >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R >>", "<< /Contents <0000> /Reference [<< /TransformMethod /DocMDP /TransformParams << >> >>] >>"}, "1 0 R"))
	require.Equal(t, parser.Success, e)
	got, err := d.Signatures()
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, 2, got[0].DocMDPPermission)
}

func TestSignatureSourceProvenance(t *testing.T) {
	for _, tc := range []struct {
		name, contents string
		indirect       bool
		decoded        []byte
	}{
		{"literal", `(a\(b\)\000)`, false, []byte{'a', '(', 'b', ')', 0}},
		{"hex", `<61 62 00 00>`, false, []byte{'a', 'b', 0, 0}},
		{"indirect", `<abcd>`, true, []byte{0xab, 0xcd}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contents := tc.contents
			if tc.indirect {
				contents = "6 0 R"
			}
			bodies := []string{"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R >>", "<< /FT /Sig /T (signed) /V 5 0 R >>", "<< /Contents " + contents + " /ByteRange [0 1 2 3] >>"}
			if tc.indirect {
				bodies = append(bodies, tc.contents)
			}
			original := append([]byte("preamble\n"), buildPDF(bodies, "1 0 R")...)
			d, e := Open(original)
			require.Equal(t, parser.Success, e)
			sig, err := d.Signatures()
			require.NoError(t, err)
			require.Len(t, sig, 1)
			require.Equal(t, tc.decoded, sig[0].Contents)
			require.Equal(t, tc.contents, string(original[sig[0].ContentsStart:sig[0].ContentsEnd]))
			source := d.SourceBytes()
			source[0] = 'X'
			original[0] = 'Y'
			require.Equal(t, byte('p'), d.SourceBytes()[0])
		})
	}
}

func TestClaimedSigningTime(t *testing.T) {
	require.Nil(t, signatureTime("D:20261109120000"))
	require.Nil(t, signatureTime("not a date"))
	tm := signatureTime("D:20261109120000+05'30'")
	require.NotNil(t, tm)
	require.Equal(t, time.Date(2026, 11, 9, 6, 30, 0, 0, time.UTC), tm.UTC())
}

func TestCompressedSignatureHasNoOriginalStringSpan(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, 8)
	write := func(id int, body string) {
		offsets[id] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", id, body)
	}
	write(1, "<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>")
	write(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	write(3, "<< /Type /Page /Parent 2 0 R >>")
	write(4, "<< /FT /Sig /T (compressed) /V 5 0 R >>")
	data := "5 0 << /Contents <abcd> /ByteRange [0 1 2 3] >>"
	write(6, fmt.Sprintf("<< /Type /ObjStm /N 1 /First 4 /Length %d >>\nstream\n%s\nendstream", len(data), data))
	offsets[7] = buf.Len()
	var entries bytes.Buffer
	for i := 0; i < 8; i++ {
		var entry [7]byte
		switch i {
		case 0:
			binary.BigEndian.PutUint16(entry[5:], 65535)
		case 5:
			entry[0] = 2
			binary.BigEndian.PutUint32(entry[1:], 6)
		default:
			entry[0] = 1
			binary.BigEndian.PutUint32(entry[1:], uint32(offsets[i]))
		}
		entries.Write(entry[:])
	}
	fmt.Fprintf(&buf, "7 0 obj\n<< /Type /XRef /Size 8 /Root 1 0 R /W [1 4 2] /Length %d >>\nstream\n", entries.Len())
	buf.Write(entries.Bytes())
	fmt.Fprintf(&buf, "\nendstream\nendobj\nstartxref\n%d\n%%%%EOF\n", offsets[7])
	d, e := Open(buf.Bytes())
	require.Equal(t, parser.Success, e)
	got, err := d.Signatures()
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, []byte{0xab, 0xcd}, got[0].Contents)
	require.Equal(t, int64(-1), got[0].ContentsStart)
	require.Equal(t, int64(-1), got[0].ContentsEnd)
}

func TestUsageRightsSignaturesAreInspected(t *testing.T) {
	for _, key := range []string{"UR", "UR3"} {
		t.Run(key, func(t *testing.T) {
			d, e := Open(buildPDF([]string{"<< /Type /Catalog /Pages 2 0 R /Perms << /" + key + " 4 0 R >> >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R >>", "<< /Type /Sig /Contents <abcd> >>"}, "1 0 R"))
			require.Equal(t, parser.Success, e)
			signatures, err := d.Signatures()
			require.NoError(t, err)
			require.Len(t, signatures, 1)
			require.Equal(t, []byte{0xab, 0xcd}, signatures[0].Contents)
		})
	}
}
func TestPermsSignatureReferencesAreDeduplicated(t *testing.T) {
	d, e := Open(buildPDF([]string{"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [6 0 R] >> /Perms << /DocMDP 4 0 R /UR 4 0 R /UR3 5 0 R >> >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R >>", "<< /Type /Sig /Contents <abcd> >>", "<< /Type /Sig /Contents <ef01> >>", "<< /FT /Sig /T (approval) /V 4 0 R >>"}, "1 0 R"))
	require.Equal(t, parser.Success, e)
	signatures, err := d.Signatures()
	require.NoError(t, err)
	require.Len(t, signatures, 2)
	require.Equal(t, "approval", signatures[0].FieldName)
}
