package document

import (
	"bytes"
	"context"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ErrSignatureCapacity means the CMS value does not fit its reserved PDF slot.
var ErrSignatureCapacity = errors.New("signature exceeds reserved Contents capacity")

// SigningOptions configures an invisible detached signature field.
type SigningOptions struct {
	FieldName    string
	ContentsSize int
	Name         string
	Reason       string
	Location     string
	SigningTime  time.Time
}

// PreparedSignature holds the exact revision bytes to be signed. Exported
// offsets are informational; changing them does not alter finalization.
type PreparedSignature struct {
	FieldName     string
	ByteRange     [4]int64
	ContentsStart int64
	ContentsEnd   int64
	data          []byte
	gapStart      int
	gapEnd        int
}

// PrepareSignature reserves an incremental signature without invoking a key.
// Approval-signed inputs retain every original byte. Encrypted documents and
// signature-policy restrictions are rejected; unsigned edits are snapshotted.
func (d *Document) PrepareSignature(ctx context.Context, opts SigningOptions) (*PreparedSignature, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d == nil || d.prs == nil {
		return nil, fmt.Errorf("prepare signature: nil document")
	}
	base, source, err := d.signingSource(ctx)
	if err != nil {
		return nil, err
	}
	opts, err = base.normalizeSigningOptions(opts)
	if err != nil {
		return nil, err
	}
	previous, err := signingPreviousXref(source)
	if err != nil {
		return nil, err
	}
	return base.appendSignature(ctx, source, previous, opts)
}

// SignedBytes returns a copy of the two byte ranges covered by the signature.
func (p *PreparedSignature) SignedBytes() []byte {
	if p == nil {
		return nil
	}
	result := make([]byte, 0, len(p.data)-(p.gapEnd-p.gapStart))
	result = append(result, p.data[:p.gapStart]...)
	return append(result, p.data[p.gapEnd:]...)
}

// Finalize inserts one DER sequence and zero-pads the reserved slot. It never
// modifies the prepared revision, so failed or repeated calls are isolated.
func (p *PreparedSignature) Finalize(der []byte) ([]byte, error) {
	if p == nil || p.gapEnd-p.gapStart < 2 {
		return nil, fmt.Errorf("finalize signature: invalid preparation")
	}
	capacity := (p.gapEnd - p.gapStart - 2) / 2
	if len(der) > capacity {
		return nil, ErrSignatureCapacity
	}
	var envelope asn1.RawValue
	rest, err := asn1.Unmarshal(der, &envelope)
	if err != nil || len(rest) != 0 || envelope.Class != 0 || envelope.Tag != asn1.TagSequence || !envelope.IsCompound {
		return nil, fmt.Errorf("finalize signature: expected one DER sequence")
	}
	result := bytes.Clone(p.data)
	hex.Encode(result[p.gapStart+1:p.gapStart+1+hex.EncodedLen(len(der))], der)
	return result, nil
}
