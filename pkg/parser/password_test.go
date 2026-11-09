package parser_test

import (
	"bytes"
	"context"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	"github.com/stretchr/testify/require"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPasswordConformance(t *testing.T) {
	plain, err := os.ReadFile("testdata/security/plain.pdf")
	require.NoError(t, err)
	baseline, err := (&parser.Backend{}).OpenBytes(context.Background(), plain)
	require.NoError(t, err)
	pg, err := baseline.Page(context.Background(), 0)
	require.NoError(t, err)
	expectedCells, err := pg.TextCells(context.Background())
	require.NoError(t, err)
	for _, name := range []string{"r2", "r3", "r4", "r5", "r6", "r4-clear-metadata", "r6-object-streams"} {
		for _, password := range []string{"user", "owner"} {
			t.Run(name+"/"+password, func(t *testing.T) {
				data, err := os.ReadFile("testdata/security/" + name + ".pdf")
				require.NoError(t, err)
				d, err := (&parser.Backend{}).OpenBytesWithPassword(context.Background(), data, password)
				require.NoError(t, err)
				native := d.(*parser.Document)
				fields, err := native.AcroFormValues(context.Background())
				require.NoError(t, err)
				require.Equal(t, "Hidden value", fields["private"])
				page, err := native.Page(context.Background(), 0)
				require.NoError(t, err)
				cells, err := page.TextCells(context.Background())
				require.NoError(t, err)
				require.NotEmpty(t, cells)
				require.Equal(t, expectedCells, cells)
				var out bytes.Buffer
				require.NoError(t, native.WritePDF(context.Background(), &out))

				reopened, err := (&parser.Backend{}).OpenBytes(context.Background(), out.Bytes())
				require.NoError(t, err)
				values, err := reopened.(*parser.Document).AcroFormValues(context.Background())
				require.NoError(t, err)
				require.Equal(t, fields, values)
			})
		}
	}
}

func TestPasswordErrorsAndEncryptionDetection(t *testing.T) {
	ctx := context.Background()
	encrypted, err := os.ReadFile("testdata/security/r6.pdf")
	require.NoError(t, err)
	backend := &parser.Backend{}
	_, err = backend.OpenBytes(ctx, encrypted)
	require.ErrorIs(t, err, parser.ErrPasswordRequired)
	_, err = backend.OpenBytesWithPassword(ctx, encrypted, "wrong")
	require.ErrorIs(t, err, parser.ErrWrongPassword)
	unsupported := bytes.Replace(encrypted, []byte("/R 6"), []byte("/R 9"), 1)
	_, err = backend.OpenBytesWithPassword(ctx, unsupported, "user")
	require.ErrorIs(t, err, parser.ErrUnsupportedEncryption)
	malformed := bytes.Replace(encrypted, []byte("/UE"), []byte("/ZZ"), 1)
	_, err = backend.OpenBytesWithPassword(ctx, malformed, "user")
	require.ErrorIs(t, err, parser.ErrInvalidPDF)
	mixed := bytes.Replace(encrypted, []byte("/StmF /StdCF"), []byte("/StmF /NoCFx"), 1)
	_, err = backend.OpenBytesWithPassword(ctx, mixed, "user")
	require.ErrorIs(t, err, parser.ErrUnsupportedEncryption)
	_, err = backend.OpenBytes(ctx, []byte("garbage"))
	require.ErrorIs(t, err, parser.ErrInvalidPDF)
	for _, tc := range []struct {
		name      string
		encrypted bool
	}{{"plain", false}, {"r6-empty", true}} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile("testdata/security/" + tc.name + ".pdf")
			require.NoError(t, err)
			doc, err := backend.OpenBytes(ctx, data)
			require.NoError(t, err)
			actual, err := doc.(*parser.Document).IsEncrypted(ctx)
			require.NoError(t, err)
			require.Equal(t, tc.encrypted, actual)
		})
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = backend.OpenBytesWithPassword(canceled, encrypted, "user")
	require.ErrorIs(t, err, context.Canceled)
}

func TestUnicodePasswords(t *testing.T) {
	for _, tc := range []struct{ name, password string }{{"r3-unicode", "café"}, {"r6-unicode", "秘密"}} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile("testdata/security/" + tc.name + ".pdf")
			require.NoError(t, err)
			doc, err := (&parser.Backend{}).OpenBytesWithPassword(context.Background(), data, tc.password)
			require.NoError(t, err)
			values, err := doc.(*parser.Document).AcroFormValues(context.Background())
			require.NoError(t, err)
			require.Equal(t, "Hidden value", values["private"])
		})
	}
}

func TestEncryptedExportsWithQPDF(t *testing.T) {
	qpdf, err := exec.LookPath("qpdf")
	if err != nil {
		t.Skip("qpdf not installed; independent export validation")
	}
	for _, name := range []string{"r2", "r3", "r4", "r5", "r6", "r6-object-streams", "r4-clear-metadata"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile("testdata/security/" + name + ".pdf")
			require.NoError(t, err)
			doc, err := (&parser.Backend{}).OpenBytesWithPassword(context.Background(), data, "user")
			require.NoError(t, err)
			native := doc.(*parser.Document)
			for _, subset := range []bool{false, true} {
				var out bytes.Buffer
				if subset {
					err = native.WritePages(context.Background(), &out, []int{0})
				} else {
					err = native.WritePDF(context.Background(), &out)
				}
				require.NoError(t, err)
				path := filepath.Join(t.TempDir(), "plain.pdf")
				require.NoError(t, os.WriteFile(path, out.Bytes(), 0600))
				result, err := exec.Command(qpdf, "--check", path).CombinedOutput()
				require.NoError(t, err, string(result))
				require.Contains(t, string(result), "File is not encrypted")
			}
		})
	}
}

func TestMalformedEncryptedStreamFailsWithoutWriting(t *testing.T) {
	data, err := os.ReadFile("testdata/security/r4.pdf")
	require.NoError(t, err)
	begin := bytes.Index(data, []byte("stream\n")) + len("stream\n")
	require.GreaterOrEqual(t, begin, len("stream\n"))
	end := bytes.Index(data[begin:], []byte("endstream")) + begin
	require.Greater(t, end, begin)
	for i := begin; i < end-1; i++ {
		data[i] = 0
	}
	doc, err := (&parser.Backend{}).OpenBytesWithPassword(context.Background(), data, "user")
	require.NoError(t, err)
	var out bytes.Buffer
	require.Error(t, doc.(*parser.Document).WritePDF(context.Background(), &out))
	require.Zero(t, out.Len())
}
