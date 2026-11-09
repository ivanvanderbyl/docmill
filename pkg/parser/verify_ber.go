package parser

import (
	"bytes"
	"encoding/asn1"
	"errors"

	"github.com/digitorus/pkcs7"
)

// This frames the outer BER containers without implementing CMS. Complete CMS
// parsing and BER normalization remain with the pinned pkcs7 dependency. The
// frame boundary is needed because that parser ignores trailing input bytes.
type cmsFrame struct {
	tag           byte
	content, full []byte
}

func cmsFrameAt(data []byte, depth int) (cmsFrame, []byte, error) {
	if depth > 64 || len(data) < 2 || data[0] == 0 {
		return cmsFrame{}, nil, errors.New("malformed CMS BER frame")
	}
	pos, err := cmsTagEnd(data)
	if err != nil {
		return cmsFrame{}, nil, err
	}
	lengthByte := data[pos]
	pos++
	if lengthByte == 128 {
		return cmsIndefiniteFrame(data, pos, depth)
	}
	return cmsDefiniteFrame(data, pos, lengthByte)
}

func cmsTagEnd(data []byte) (int, error) {
	pos := 1
	if data[0]&31 == 31 {
		for {
			if pos >= len(data) || pos > 8 {
				return 0, errors.New("malformed CMS BER tag")
			}
			b := data[pos]
			pos++
			if b&128 == 0 {
				break
			}
		}
	}
	if pos >= len(data) {
		return 0, errors.New("missing CMS BER length")
	}
	return pos, nil
}

func cmsIndefiniteFrame(data []byte, start, depth int) (cmsFrame, []byte, error) {
	if data[0]&32 == 0 {
		return cmsFrame{}, nil, errors.New("indefinite primitive CMS BER value")
	}
	pos := start
	for {
		if len(data)-pos < 2 {
			return cmsFrame{}, nil, errors.New("missing CMS BER end marker")
		}
		if data[pos] == 0 && data[pos+1] == 0 {
			return cmsFrame{tag: data[0], content: data[start:pos], full: data[:pos+2]}, data[pos+2:], nil
		}
		child, _, err := cmsFrameAt(data[pos:], depth+1)
		if err != nil {
			return cmsFrame{}, nil, err
		}
		pos += len(child.full)
	}
}

func cmsDefiniteFrame(data []byte, pos int, lengthByte byte) (cmsFrame, []byte, error) {
	length := uint64(lengthByte)
	if lengthByte&128 != 0 {
		count := int(lengthByte & 127)
		if count == 0 || count > 8 || count > len(data)-pos {
			return cmsFrame{}, nil, errors.New("malformed CMS BER length")
		}
		length = 0
		for _, b := range data[pos : pos+count] {
			length = length<<8 | uint64(b)
		}
		pos += count
	}
	if length > uint64(len(data)-pos) {
		return cmsFrame{}, nil, errors.New("CMS BER value crosses its enclosing boundary")
	}
	end := pos + int(length)
	return cmsFrame{tag: data[0], content: data[pos:end], full: data[:end]}, data[end:], nil
}

func cmsEnvelopeBytes(padded []byte) ([]byte, error) {
	if len(padded) > 16<<20 {
		return nil, errors.New("CMS signature exceeds 16 MiB")
	}
	frame, rest, err := cmsFrameAt(padded, 0)
	if err != nil {
		return nil, err
	}
	if frame.tag != 0x30 {
		return nil, errors.New("CMS is not a sequence")
	}
	if len(bytes.Trim(rest, "\x00")) != 0 {
		return nil, errors.New("nonzero data after CMS signature")
	}
	return frame.full, nil
}

func frameOID(frame cmsFrame) (asn1.ObjectIdentifier, error) {
	var oid asn1.ObjectIdentifier
	rest, err := asn1.Unmarshal(frame.full, &oid)
	if len(rest) != 0 {
		return nil, errors.New("invalid CMS content OID")
	}
	return oid, err
}

func checkDetachedContent(encoded []byte) error {
	envelope, err := unwrapCMSFrame(encoded, 0x30)
	if err != nil {
		return err
	}
	typ, rest, err := cmsFrameAt(envelope.content, 0)
	if err != nil {
		return err
	}
	oid, err := frameOID(typ)
	if err != nil || !oid.Equal(pkcs7.OIDSignedData) {
		return errors.New("CMS is not SignedData")
	}
	wrapped, err := unwrapCMSFrame(rest, 0xa0)
	if err != nil {
		return err
	}
	signed, err := unwrapCMSFrame(wrapped.content, 0x30)
	if err != nil {
		return err
	}
	return checkCMSDataContent(signed.content)
}

func unwrapCMSFrame(data []byte, tag byte) (cmsFrame, error) {
	frame, rest, err := cmsFrameAt(data, 0)
	if err != nil {
		return cmsFrame{}, err
	}
	if frame.tag != tag || len(rest) != 0 {
		return cmsFrame{}, errors.New("unexpected CMS wrapper")
	}
	return frame, nil
}

func checkCMSDataContent(fields []byte) error {
	for range 2 {
		_, next, err := cmsFrameAt(fields, 0)
		if err != nil {
			return err
		}
		fields = next
	}
	content, _, err := cmsFrameAt(fields, 0)
	if err != nil {
		return err
	}
	if content.tag != 0x30 {
		return errors.New("invalid CMS content-info")
	}
	contentType, rest, err := cmsFrameAt(content.content, 0)
	if err != nil {
		return err
	}
	oid, err := frameOID(contentType)
	if err != nil || !oid.Equal(pkcs7.OIDData) || len(rest) != 0 {
		return errors.New("CMS must carry detached id-data content")
	}
	return nil
}
