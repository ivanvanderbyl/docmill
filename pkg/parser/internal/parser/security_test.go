package parser

import (
	"crypto/aes"
	"encoding/binary"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestStandardSecurityConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name                                   string
		revision, version, length              int
		method, stream, stringFilter, embedded string
		want                                   Error
	}{
		{"rc4", 3, 2, 128, "", "", "", "", Success},
		{"invalid revision pair", 6, 4, 256, "AESV3", "StdCF", "StdCF", "", HandlerError},
		{"invalid rc4 length", 3, 2, 264, "", "", "", "", HandlerError},
		{"aes128", 4, 4, 128, "AESV2", "StdCF", "StdCF", "", Success},
		{"aes256", 6, 5, 256, "AESV3", "StdCF", "StdCF", "", Success},
		{"aes128 wrong length", 4, 4, 40, "AESV2", "StdCF", "StdCF", "", HandlerError},
		{"aes256 wrong revision", 4, 4, 128, "AESV3", "StdCF", "StdCF", "", HandlerError},
		{"unknown algorithm", 4, 4, 128, "Mystery", "StdCF", "StdCF", "", HandlerError},
		{"mixed filters", 4, 4, 128, "AESV2", "StdCF", "Identity", "", HandlerError},
		{"embedded filter", 4, 4, 128, "AESV2", "Identity", "Identity", "StdCF", HandlerError},
		{"identity", 4, 4, 128, "AESV2", "Identity", "Identity", "", Success},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := objects.NewDictionary()
			d.SetNewNameFor("Filter", "Standard")
			d.SetFor("V", objects.NewNumberFromInt(int32(tc.version)))
			d.SetFor("R", objects.NewNumberFromInt(int32(tc.revision)))
			d.SetFor("Length", objects.NewNumberFromInt(int32(tc.length)))
			d.SetNewNameFor("StmF", tc.stream)
			d.SetNewNameFor("StrF", tc.stringFilter)
			if tc.embedded != "" {
				d.SetNewNameFor("EFF", tc.embedded)
			}
			cf := objects.NewDictionary()
			entry := objects.NewDictionary()
			entry.SetNewNameFor("CFM", tc.method)
			cf.SetFor("StdCF", entry)
			d.SetFor("CF", cf)
			_, _, result := standardCipher(d)
			require.Equal(t, tc.want, result)
		})
	}
}

func TestIdentityCipherStillRequiresAuthentication(t *testing.T) {
	data, err := os.ReadFile("../../testdata/security/r4.pdf")
	require.NoError(t, err)
	p := New()
	p.SetPassword("user")
	holder := NewHolder(p)
	require.Equal(t, Success, p.StartParse(data, holder))
	dict := p.getEncryptDict()
	require.NotNil(t, dict)
	dict.SetNewNameFor("StmF", "Identity")
	dict.SetNewNameFor("StrF", "Identity")
	p.SetPassword("wrong")
	require.Equal(t, PasswordError, p.initSecurity())
	p.SetPassword("user")
	require.Equal(t, Success, p.initSecurity())
	require.True(t, p.security.identity)
}

func TestExplicitIdentityStreamFilter(t *testing.T) {
	dict := objects.NewDictionary()
	filters := objects.NewArray()
	filters.Append(objects.NewName("Crypt"))
	filters.Append(objects.NewName("FlateDecode"))
	dict.SetFor("Filter", filters)
	params := objects.NewArray()
	identity := objects.NewDictionary()
	identity.SetNewNameFor("Name", "Identity")
	params.Append(identity)
	params.Append(objects.NewNull())
	dict.SetFor("DecodeParms", params)
	stream := objects.NewStreamFromData([]byte("already plain encoded stream"), dict)
	stream.SetObjNum(7)
	h := &securityHandler{revision: 4, key: []byte("1234567890123456"), aes: true, metadata: true, encryptObject: 99, filterName: "StdCF"}
	_, err := h.decryptObject(stream)
	require.NoError(t, err)
	acc := objects.NewStreamAcc(stream)
	acc.LoadAllDataRaw()
	require.Equal(t, []byte("already plain encoded stream"), acc.GetSpan())
	require.Equal(t, 1, dict.GetArrayFor("Filter").Len())
	require.Equal(t, "FlateDecode", dict.GetArrayFor("Filter").GetByteStringAt(0))
	require.Equal(t, 1, dict.GetArrayFor("DecodeParms").Len())
	require.Equal(t, 2, filters.Len(), "shared filter array must not be mutated")
	require.Equal(t, 2, params.Len(), "shared decode parameters must not be mutated")
}

func TestCryptFilterLengthOverridesDocumentLength(t *testing.T) {
	for _, tc := range []struct {
		name               string
		bits, length, want int
		result             Error
	}{
		{"byte length overrides bits", 40, 16, 16, Success},
		{"bit length overrides bits", 40, 128, 16, Success},
		{"zero uses document default", 128, 0, 16, Success},
		{"negative filter length rejected", 128, -16, 0, HandlerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := objects.NewDictionary()
			d.SetNewNameFor("Filter", "Standard")
			d.SetFor("V", objects.NewNumberFromInt(4))
			d.SetFor("R", objects.NewNumberFromInt(4))
			d.SetFor("Length", objects.NewNumberFromInt(int32(tc.bits)))
			d.SetNewNameFor("StmF", "StdCF")
			d.SetNewNameFor("StrF", "StdCF")
			cf := objects.NewDictionary()
			entry := objects.NewDictionary()
			entry.SetNewNameFor("CFM", "AESV2")
			entry.SetFor("Length", objects.NewNumberFromInt(int32(tc.length)))
			cf.SetFor("StdCF", entry)
			d.SetFor("CF", cf)
			_, n, e := standardCipher(d)
			require.Equal(t, tc.result, e)
			if e == Success {
				require.Equal(t, tc.want, n)
			}
		})
	}
}

// PDFium tolerates a non-F metadata marker when EncryptMetadata is true.
func TestPermissionsValidationMatchesPDFiumCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name, marker string
		permission   int32
		metadata     bool
		flag         byte
		want         bool
	}{
		{"valid", "adb", -4, true, 'T', true},
		{"invalid marker", "abc", -4, true, 'T', false},
		{"permissions mismatch", "adb", -8, true, 'T', false},
		{"unencrypted metadata", "adb", -4, false, 'F', true},
		{"metadata mismatch", "adb", -4, false, 'T', false},
		{"legacy marker tolerance", "adb", -4, true, 'X', true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := make([]byte, 32)
			block, err := aes.NewCipher(key)
			require.NoError(t, err)
			clear := make([]byte, 16)
			binary.LittleEndian.PutUint32(clear, uint32(tc.permission))
			clear[8] = tc.flag
			copy(clear[9:12], tc.marker)
			encrypted := make([]byte, 16)
			block.Encrypt(encrypted, clear)
			d := objects.NewDictionary()
			d.SetFor("P", objects.NewNumberFromInt(-4))
			d.SetFor("Perms", objects.NewString(string(encrypted), true))
			require.Equal(t, tc.want, validPermissions(d, key, tc.metadata))
		})
	}
}

func TestIndirectCryptoParametersAreMaterializedLocally(t *testing.T) {
	filters := objects.NewArray()
	filters.Append(objects.NewReference(nil, 1))
	params := objects.NewDictionary()
	params.SetFor("Predictor", objects.NewReference(nil, 2))
	dict := objects.NewDictionary()
	dict.SetFor("Filter", filters)
	dict.SetFor("DecodeParms", params)
	stream := objects.NewStreamFromData([]byte("data"), dict)
	stream.SetObjNum(7)
	resolve := func(o objects.Object) objects.Object {
		if r := objects.ToReference(o); r != nil {
			switch r.GetRefObjNum() {
			case 1:
				return objects.NewName("FlateDecode")
			case 2:
				return objects.NewNumberFromInt(1)
			}
		}
		return o
	}
	h := &securityHandler{identity: true, metadata: true, encryptObject: 99}
	_, err := h.decryptObjectWithResolver(stream, resolve)
	require.NoError(t, err)
	require.Equal(t, objects.TypeName, dict.GetArrayFor("Filter").GetObjectAt(0).Type())
	require.Equal(t, objects.TypeNumber, dict.GetDictFor("DecodeParms").GetObjectFor("Predictor").Type())
	require.Equal(t, objects.TypeReference, filters.GetObjectAt(0).Type())
	require.Equal(t, objects.TypeReference, params.GetObjectFor("Predictor").Type())
}
