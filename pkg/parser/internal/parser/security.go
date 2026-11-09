package parser

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rc4"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/binary"
	"errors"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

// Standard security algorithms follow core/fpdfapi/parser/cpdf_security_handler.cpp
// and cpdf_crypto_handler.cpp at PDFium 6688c576fd851f4159c6fae39396da50939de110
// (PDF 32000 algorithms 2, 3, 5, and 2.A/2.B).
var passwordPadding = []byte{
	0x28,
	0xbf,
	0x4e,
	0x5e,
	0x4e,
	0x75,
	0x8a,
	0x41,
	0x64,
	0,
	0x4e,
	0x56,
	0xff,
	0xfa,
	1,
	8,
	0x2e,
	0x2e,
	0,
	0xb6,
	0xd0,
	0x68,
	0x3e,
	0x80,
	0x2f,
	0x0c,
	0xa9,
	0xfe,
	0x64,
	0x53,
	0x69,
	0x7a,
}

type securityHandler struct {
	key           []byte
	aes           bool
	revision      int
	metadata      bool
	identity      bool
	encryptObject uint32
	filterName    string
}

func padPassword(password []byte) []byte {
	out := make([]byte, 32)
	n := copy(out, password)
	copy(out[n:], passwordPadding)
	return out
}

func rc4Bytes(key, data []byte) []byte {
	c, err := rc4.NewCipher(key)
	if err != nil {
		return nil
	}
	out := make([]byte, len(data))
	c.XORKeyStream(out, data)
	return out
}

func md5Bytes(data []byte) []byte {
	s := md5.Sum(data)
	return s[:]
}

func (p *Parser) initSecurity() Error {
	p.security = nil
	trailer := p.GetTrailer()
	if trailer == nil {
		return FormatError
	}
	if !trailer.KeyExist("Encrypt") {
		return Success
	}
	d := p.getEncryptDict()
	if d == nil {
		return FormatError
	}
	h, n, e := standardCipher(d)
	if e != Success {
		return e
	}
	if ref := objects.ToReference(trailer.GetObjectFor("Encrypt")); ref != nil {
		h.encryptObject = ref.GetRefObjNum()
	}
	var id []byte
	if ids := trailer.GetArrayFor("ID"); ids != nil {
		if entry := ids.GetDirectObjectAt(0); entry != nil {
			id = []byte(entry.GetString())
		}
	}
	if !validSecurityEntries(d, h.revision) {
		return FormatError
	}
	h.key = securityPasswordKey(d, []byte(p.password), id, n, h)
	if h.key == nil {
		return PasswordError
	}
	p.security = h
	return Success
}

func validSecurityEntries(d *objects.Dictionary, revision int) bool {
	if len(d.GetByteStringFor("O")) < 32 || len(d.GetByteStringFor("U")) < 16 {
		return false
	}
	if revision < 5 {
		return true
	}
	return len(d.GetByteStringFor("O")) >= 48 && len(d.GetByteStringFor("U")) >= 48 && len(d.GetByteStringFor("OE")) == 32 && len(d.GetByteStringFor("UE")) == 32 && len(d.GetByteStringFor("Perms")) == 16
}

func standardCipher(d *objects.Dictionary) (*securityHandler, int, Error) {
	v, r := d.GetIntegerFor("V"), d.GetIntegerFor("R")
	validVersion := map[int]int{2: 1, 3: 2, 4: 4, 5: 5, 6: 5}
	if d.GetNameFor("Filter") != "Standard" || validVersion[r] != v || v == 0 {
		return nil, 0, HandlerError
	}
	h := &securityHandler{revision: r, metadata: d.GetBooleanFor("EncryptMetadata", true)}
	defaults := map[int]int{1: 40, 2: 40, 4: 128, 5: 256}
	n := d.GetIntegerWithDefaultFor("Length", defaults[v]) / 8
	if v == 1 {
		n = 5
	}
	if v >= 4 {
		if e := loadCryptFilter(d, h); e != Success {
			return nil, 0, e
		}
	}
	if v == 4 && !h.identity {
		n = filterKeyLength(d, h.filterName, n)
	}
	if !validCipherLength(h, n) {
		return nil, 0, HandlerError
	}
	return h, n, Success
}

func validCipherLength(h *securityHandler, n int) bool {
	if h.revision >= 5 {
		return n == 32 && (h.aes || h.identity)
	}
	return n >= 5 && n <= 16 && (!h.aes || n == 16)
}

func filterKeyLength(d *objects.Dictionary, name string, fallback int) int {
	filters := d.GetDictFor("CF")
	entry := filters.GetDictFor(name)
	bits := entry.GetIntegerFor("Length")
	if bits == 0 {
		return fallback
	}
	if bits < 0 {
		return -1
	}
	if bits < 40 {
		return bits
	}
	return bits / 8
}

func loadCryptFilter(d *objects.Dictionary, h *securityHandler) Error {
	streamFilter, stringFilter := d.GetNameFor("StmF"), d.GetNameFor("StrF")
	if streamFilter == "" {
		streamFilter = "Identity"
	}
	if stringFilter == "" {
		stringFilter = "Identity"
	}
	if streamFilter != stringFilter {
		return HandlerError
	}
	if eff := d.GetNameFor("EFF"); eff != "" && eff != streamFilter {
		return HandlerError
	}
	h.filterName = streamFilter
	if streamFilter == "Identity" {
		h.identity = true
		return Success
	}
	filters := d.GetDictFor("CF")
	if filters == nil {
		return HandlerError
	}
	filter := filters.GetDictFor(streamFilter)
	if filter == nil {
		return HandlerError
	}
	switch filter.GetNameFor("CFM") {
	case "V2":
		if h.revision != 4 {
			return HandlerError
		}
	case "AESV2":
		if h.revision != 4 {
			return HandlerError
		}
		h.aes = true
	case "AESV3":
		if h.revision < 5 {
			return HandlerError
		}
		h.aes = true
	default:
		return HandlerError
	}
	return Success
}

func securityPasswordKey(d *objects.Dictionary, password, id []byte, n int, h *securityHandler) []byte {
	if h.revision >= 5 {
		key := modernPasswordKey(d, password, h.revision, h.metadata)
		if key == nil {
			key = modernPasswordKey(d, latin1ToUTF8(password), h.revision, h.metadata)
		}
		return key
	}
	key := legacyPasswordKey(d, password, id, n, h.revision)
	if key == nil {
		key = legacyPasswordKey(d, utf8ToLatin1(string(password)), id, n, h.revision)
	}
	return key
}

type legacySecurity struct {
	dict     *objects.Dictionary
	id       []byte
	length   int
	revision int
}

func legacyPasswordKey(d *objects.Dictionary, password, id []byte, n, revision int) []byte {
	c := legacySecurity{dict: d, id: id, length: n, revision: revision}
	if len(password) > 0 {
		if key := c.userKey(c.ownerUser(password)); key != nil {
			return key
		}
	}
	return c.userKey(password)
}

func (c legacySecurity) fileKey(password []byte, ignoreMetadata bool) []byte {
	input := append(padPassword(password), []byte(c.dict.GetByteStringFor("O"))...)
	var perm [4]byte
	binary.LittleEndian.PutUint32(perm[:], uint32(c.dict.GetIntegerFor("P")))
	input = append(input, perm[:]...)
	input = append(input, c.id...)
	if c.revision >= 3 && !ignoreMetadata && !c.dict.GetBooleanFor("EncryptMetadata", true) {
		input = append(input, 255, 255, 255, 255)
	}
	digest := md5Bytes(input)
	if c.revision >= 3 {
		for range 50 {
			digest = md5Bytes(digest[:c.length])
		}
	}
	return append([]byte{}, digest[:c.length]...)
}

func xorKey(key []byte, value byte) []byte {
	out := append([]byte{}, key...)
	for i := range out {
		out[i] ^= value
	}
	return out
}

func (c legacySecurity) userKey(password []byte) []byte {
	user := []byte(c.dict.GetByteStringFor("U"))
	for _, ignoreMetadata := range []bool{false, true} {
		key := c.fileKey(password, ignoreMetadata)
		expected := rc4Bytes(key, passwordPadding)
		if c.revision >= 3 {
			expected = md5Bytes(append(append([]byte{}, passwordPadding...), c.id...))
			for i := range 20 {
				expected = rc4Bytes(xorKey(key, byte(i)), expected)
			}
		}
		if subtle.ConstantTimeCompare(expected[:16], user[:16]) == 1 {
			return key
		}
	}
	return nil
}

func (c legacySecurity) ownerUser(password []byte) []byte {
	digest := md5Bytes(padPassword(password))
	if c.revision >= 3 {
		for range 50 {
			digest = md5Bytes(digest)
		}
	}
	owner := []byte(c.dict.GetByteStringFor("O"))[:32]
	if c.revision == 2 {
		return rc4Bytes(digest[:c.length], owner)
	}
	for i := 19; i >= 0; i-- {
		owner = rc4Bytes(xorKey(digest[:c.length], byte(i)), owner)
	}
	return owner
}

func modernHash(password, salt, user []byte, revision int) []byte {
	input := append(append(append([]byte{}, password...), salt...), user...)
	digest := sha256.Sum256(input)
	k := digest[:]
	if revision == 5 {
		return k
	}
	for i := 0; ; i++ {
		chunk := append(append(append([]byte{}, password...), k...), user...)
		content := bytes.Repeat(chunk, 64)
		block, _ := aes.NewCipher(k[:16])
		cipher.NewCBCEncrypter(block, k[16:32]).CryptBlocks(content, content)
		mod := 0
		for _, b := range content[:16] {
			mod = (mod*256 + int(b)) % 3
		}
		switch mod {
		case 0:
			s := sha256.Sum256(content)
			k = s[:]
		case 1:
			s := sha512.Sum384(content)
			k = s[:]
		case 2:
			s := sha512.Sum512(content)
			k = s[:]
		}
		if i >= 63 && i >= int(content[len(content)-1])+31 {
			return append([]byte{}, k[:32]...)
		}
	}
}

func decryptCBC(key, iv, data []byte) ([]byte, error) {
	if len(data)%aes.BlockSize != 0 {
		return nil, errors.New("invalid AES block length")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)
	return out, nil
}

func modernPasswordKey(d *objects.Dictionary, password []byte, revision int, metadata bool) []byte {
	if len(password) > 127 {
		password = password[:127]
	}
	owner, user := []byte(d.GetByteStringFor("O")), []byte(d.GetByteStringFor("U"))
	if len(owner) < 48 || len(user) < 48 {
		return nil
	}
	for _, isOwner := range []bool{true, false} {
		if isOwner && len(password) == 0 {
			continue
		}
		entry := user
		var extra []byte
		keyEntry := "UE"
		if isOwner {
			entry = owner
			extra = user[:48]
			keyEntry = "OE"
		}
		if subtle.ConstantTimeCompare(modernHash(password, entry[32:40], extra, revision), entry[:32]) != 1 {
			continue
		}
		wrapped := []byte(d.GetByteStringFor(keyEntry))
		if len(wrapped) != 32 {
			return nil
		}
		key, err := decryptCBC(modernHash(password, entry[40:48], extra, revision), make([]byte, 16), wrapped)
		if err != nil {
			return nil
		}
		if !validPermissions(d, key, metadata) {
			return nil
		}
		return key
	}
	return nil
}

func validPermissions(d *objects.Dictionary, key []byte, metadata bool) bool {
	perms := []byte(d.GetByteStringFor("Perms"))
	if len(perms) != 16 {
		return false
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return false
	}
	decoded := make([]byte, 16)
	block.Decrypt(decoded, perms)
	if string(decoded[9:12]) != "adb" || binary.LittleEndian.Uint32(decoded[:4]) != uint32(d.GetIntegerFor("P")) {
		return false
	}
	return decoded[8] == 'F' || metadata
}

func (h *securityHandler) decrypt(data []byte, objnum, gennum uint32) ([]byte, error) {
	if h.identity {
		return append([]byte{}, data...), nil
	}
	key := h.key
	if h.revision < 5 {
		salted := append([]byte{}, key...)
		salted = append(salted, byte(objnum), byte(objnum>>8), byte(objnum>>16), byte(gennum), byte(gennum>>8))
		if h.aes {
			salted = append(salted, []byte("sAlT")...)
		}
		key = md5Bytes(salted)[:min(len(key)+5, 16)]
	}
	if !h.aes {
		return rc4Bytes(key, data), nil
	}
	if len(data) < 32 {
		return nil, errors.New("invalid AES ciphertext")
	}
	out, err := decryptCBC(key, data[:16], data[16:])
	if err != nil {
		return nil, err
	}
	padding := int(out[len(out)-1])
	if padding < 1 || padding > 16 || padding > len(out) {
		return nil, errors.New("invalid AES padding")
	}
	for _, b := range out[len(out)-padding:] {
		if int(b) != padding {
			return nil, errors.New("invalid AES padding")
		}
	}
	return out[:len(out)-padding], nil
}

func (h *securityHandler) decryptObject(obj objects.Object) (objects.Object, error) {
	return h.decryptObjectWithResolver(obj, objects.Direct)
}

func (h *securityHandler) decryptObjectWithResolver(obj objects.Object, resolve func(objects.Object) objects.Object) (objects.Object, error) {
	if obj.GetObjNum() == h.encryptObject {
		return obj, nil
	}
	walker := objectDecryptor{
		handler:    h,
		number:     obj.GetObjNum(),
		generation: obj.GetGenNum(),
		visited:    make(map[objects.Object]bool),
		resolve:    resolve,
	}
	return walker.walk(obj)
}

type objectDecryptor struct {
	resolve    func(objects.Object) objects.Object
	handler    *securityHandler
	number     uint32
	generation uint32
	visited    map[objects.Object]bool
}

func (w *objectDecryptor) walk(v objects.Object) (objects.Object, error) {
	if v == nil || w.visited[v] {
		return v, nil
	}
	w.visited[v] = true
	switch x := v.(type) {
	case *objects.String:
		data, err := w.handler.decrypt([]byte(x.GetString()), w.number, w.generation)
		if err != nil {
			return nil, err
		}
		s := objects.NewString(string(data), x.IsHex())
		s.SetObjNum(x.GetObjNum())
		s.SetGenNum(x.GetGenNum())
		return s, nil
	case *objects.Array:
		for i := 0; i < x.Len(); i++ {
			child, err := w.walk(x.GetObjectAt(i))
			if err != nil {
				return nil, err
			}
			x.SetAt(i, child)
		}
	case *objects.Dictionary:
		return x, w.dictionary(x)
	case *objects.Stream:
		return x, w.stream(x)
	}
	return v, nil
}

func (w *objectDecryptor) dictionary(d *objects.Dictionary) error {
	for _, key := range d.GetKeys() {
		if key == "Contents" && (w.name(d, "Type") == "Sig" || w.name(d, "FT") == "Sig") {
			continue
		}
		child, err := w.walk(d.GetObjectFor(key))
		if err != nil {
			return err
		}
		d.SetFor(key, child)
	}
	return nil
}

func (w *objectDecryptor) stream(s *objects.Stream) error {
	if w.name(s.Dict(), "Type") == "XRef" {
		return nil
	}
	if err := w.dictionary(s.Dict()); err != nil {
		return err
	}
	if !w.handler.metadata && w.name(s.Dict(), "Type") == "Metadata" {
		return nil
	}
	decrypt, err := w.handler.streamEncryption(s.Dict(), w.resolve)
	if err != nil {
		return err
	}
	if !decrypt {
		return nil
	}
	acc := objects.NewStreamAcc(s)
	acc.LoadAllDataRaw()
	data, err := w.handler.decrypt(acc.GetSpan(), w.number, w.generation)
	if err != nil {
		return err
	}
	s.SetData(data)
	return nil
}

// An explicit Crypt filter overrides the default stream filter. It must run
// before other filters, so remove it after handling encryption here.
func (h *securityHandler) streamEncryption(d *objects.Dictionary, resolve func(objects.Object) objects.Object) (bool, error) {
	if err := materializeCryptoParameters(d, resolve); err != nil {
		return false, err
	}
	filter := resolve(d.GetObjectFor("Filter"))
	if filter == nil {
		return true, nil
	}
	name := filter.GetString()
	filters := objects.ToArray(filter)
	if filters != nil {
		if hasNonleadingCrypt(filters, resolve) {
			return false, errors.New("Crypt filter must be first")
		}
		first := resolve(filters.GetObjectAt(0))
		if first == nil {
			return true, nil
		}
		name = first.GetString()
	}
	if name != "Crypt" {
		return true, nil
	}
	paramsObject := resolve(d.GetObjectFor("DecodeParms"))
	params := objects.ToDictionary(paramsObject)
	paramArray := objects.ToArray(paramsObject)
	if paramArray != nil {
		params = objects.ToDictionary(resolve(paramArray.GetObjectAt(0)))
	}
	selected := selectedCryptFilter(params, resolve)
	if selected != "Identity" && selected != h.filterName {
		return false, errors.New("unsupported stream crypt filter")
	}
	if filters == nil || filters.Len() == 1 {
		d.RemoveFor("Filter")
		d.RemoveFor("DecodeParms")
	} else {
		filters = objects.ToArray(filters.Clone())
		filters.RemoveAt(0)
		d.SetFor("Filter", filters)
		if paramArray != nil {
			paramArray = objects.ToArray(paramArray.Clone())
			paramArray.RemoveAt(0)
			d.SetFor("DecodeParms", paramArray)
		}
	}
	return selected != "Identity", nil
}

func utf8ToLatin1(password string) []byte {
	out := make([]byte, 0, len(password))
	for _, r := range password {
		if r <= 255 {
			out = append(out, byte(r))
		} else {
			out = append(out, byte(r&255))
		}
	}
	return out
}

func latin1ToUTF8(password []byte) []byte {
	runes := make([]rune, len(password))
	for i, b := range password {
		runes[i] = rune(b)
	}
	return []byte(string(runes))
}

func hasNonleadingCrypt(filters *objects.Array, resolve func(objects.Object) objects.Object) bool {
	for i := 1; i < filters.Len(); i++ {
		if item := resolve(filters.GetObjectAt(i)); item != nil && item.GetString() == "Crypt" {
			return true
		}
	}
	return false
}

func (w *objectDecryptor) name(d *objects.Dictionary, key string) string {
	obj := w.resolve(d.GetObjectFor(key))
	if obj == nil {
		return ""
	}
	return obj.GetString()
}

// Crypto runs inside the holder's serialized parser call. Resolving through the
// public holder here would reacquire its mutex. ParseIndirectObject retains the
// parser's object-number cycle guard without taking that mutex again.
func (p *Parser) resolveCryptoObject(obj objects.Object) objects.Object {
	seen := make(map[uint32]bool)
	for range 100 {
		ref := objects.ToReference(obj)
		if ref == nil {
			return obj
		}
		id := ref.GetRefObjNum()
		if seen[id] {
			return nil
		}
		seen[id] = true
		obj = p.ParseIndirectObject(id)
	}
	return nil
}

// Object-stream filter decoding also runs under the parser lock. Materialize
// its small parameter graph now so the codec never re-enters the holder.
func directCryptoParameter(obj objects.Object, resolve func(objects.Object) objects.Object, active map[objects.Object]bool, depth int) (objects.Object, error) {
	if depth > 100 {
		return nil, errors.New("crypto parameter nesting limit")
	}
	obj = resolve(obj)
	if obj == nil {
		return nil, nil
	}
	if active[obj] {
		return nil, errors.New("cyclic crypto parameters")
	}
	active[obj] = true
	defer delete(active, obj)
	switch v := obj.(type) {
	case *objects.Array:
		out := objects.NewArray()
		for i := 0; i < v.Len(); i++ {
			item, err := directCryptoParameter(v.GetObjectAt(i), resolve, active, depth+1)
			if err != nil {
				return nil, err
			}
			out.Append(item)
		}
		return out, nil
	case *objects.Dictionary:
		out := objects.NewDictionary()
		for _, key := range v.GetKeys() {
			item, err := directCryptoParameter(v.GetObjectFor(key), resolve, active, depth+1)
			if err != nil {
				return nil, err
			}
			out.SetFor(key, item)
		}
		return out, nil
	default:
		return obj.Clone(), nil
	}
}

func materializeCryptoParameters(d *objects.Dictionary, resolve func(objects.Object) objects.Object) error {
	for _, key := range []string{"Filter", "DecodeParms"} {
		if !d.KeyExist(key) {
			continue
		}
		direct, err := directCryptoParameter(d.GetObjectFor(key), resolve, make(map[objects.Object]bool), 0)
		if err != nil {
			return err
		}
		d.SetFor(key, direct)
	}
	return nil
}

func selectedCryptFilter(params *objects.Dictionary, resolve func(objects.Object) objects.Object) string {
	if params != nil {
		if name := resolve(params.GetObjectFor("Name")); name != nil && name.GetString() != "" {
			return name.GetString()
		}
	}
	return "Identity"
}
