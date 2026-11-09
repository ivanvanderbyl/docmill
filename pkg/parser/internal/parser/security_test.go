package parser

import (
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
}
