// Ported from core/fxcrt/fx_number.{h,cpp} and the StringToFloat helper from
// core/fxcrt/fx_string.cpp @ pdfium 0db284a42.
//
// FX_Number is the value the content-stream lexer produces for numeric tokens:
// it is parsed as an unsigned int, a signed int, or a float, with PDFium's
// exact int-vs-float detection and overflow handling (which the encryption
// Permissions flag relies on — Table 3.20, PDF 1.7). The C++ std::variant is
// modelled as a kind tag plus the three typed fields. See plan 009 Phase A.
package crt

import (
	"bytes"
	"math"
	"strconv"
	"strings"
	"unsafe"
)

type numberKind uint8

const (
	numberUnsigned numberKind = iota // uint32
	numberSigned                     // int32
	numberFloat                      // float32
)

const (
	int32Max = int32(math.MaxInt32) // 2147483647
	int32Min = int32(math.MinInt32) // -2147483648
)

// Number mirrors FX_Number: a numeric value that remembers whether it was an
// unsigned int, a signed int, or a float.
type Number struct {
	kind numberKind
	u    uint32
	i    int32
	f    float32
}

// NumberFromInt builds a signed Number (FX_Number(int32_t)).
func NumberFromInt(value int32) Number {
	return Number{kind: numberSigned, i: value}
}

// NumberFromFloat builds a float Number (FX_Number(float)).
func NumberFromFloat(value float32) Number {
	return Number{kind: numberFloat, f: value}
}

// NumberFromString parses a numeric token exactly as FX_Number(ByteStringView).
// An empty string yields the default unsigned zero.
func NumberFromString(s string) Number {
	if len(s) == 0 {
		return Number{kind: numberUnsigned, u: 0}
	}

	if strings.IndexByte(s, '.') >= 0 {
		return Number{kind: numberFloat, f: StringToFloat32(s)}
	}

	// Numbers in PDF are typically 123, -123, etc. But the encryption
	// Permissions value is an unsigned quantity, so accumulate as uint32 and
	// only reinterpret as signed when an explicit sign is present.
	var acc uint64
	overflow := false
	signed := false
	negative := false
	cc := 0
	switch s[0] {
	case '+':
		signed = true
		cc++
	case '-':
		signed = true
		negative = true
		cc++
	}

	for ; cc < len(s) && isDecimalDigit(s[cc]); cc++ {
		if !overflow {
			acc = acc*10 + uint64(s[cc]-'0')
			if acc > math.MaxUint32 {
				overflow = true
			}
		}
	}

	// FX_SAFE_UINT32 becomes invalid on overflow; ValueOrDefault(0) -> 0.
	var uValue uint32
	if !overflow {
		uValue = uint32(acc)
	}

	if !signed {
		return Number{kind: numberUnsigned, u: uValue}
	}

	// With a sign, reset to 0 when the magnitude exceeds the signed range.
	const uLimit = uint32(math.MaxInt32) // 2147483647
	limit := uLimit
	if negative {
		limit = uLimit + 1
	}
	if uValue > limit {
		uValue = 0
	}

	value := int32(uValue) // wraps for uValue == 2147483648 -> int32Min, as in C++.
	if negative {
		// |value| is positive except for the corner case "-2147483648", where
		// value is already int32Min and negating it would overflow.
		if value != int32Min {
			value = -value
		} else {
			value = int32Min
		}
	}
	return Number{kind: numberSigned, i: value}
}

// NumberFromBytes parses a content-stream numeric token without copying the
// tokenizer's reusable byte buffer. Valid decimal tokens take a single
// validation pass; malformed tokens fall back to NumberFromString semantics.
func NumberFromBytes(s []byte) Number {
	if len(s) == 0 {
		return Number{kind: numberUnsigned, u: 0}
	}

	if bytes.IndexByte(s, '.') >= 0 {
		view := unsafe.String(unsafe.SliceData(s), len(s))
		value, err := strconv.ParseFloat(view, 32)
		if err != nil {
			if numErr, ok := err.(*strconv.NumError); ok && numErr.Err == strconv.ErrRange {
				return Number{kind: numberFloat, f: float32(value)}
			}
			return Number{kind: numberFloat, f: StringToFloat32(view)}
		}
		return Number{kind: numberFloat, f: float32(value)}
	}

	var acc uint64
	overflow := false
	signed := false
	negative := false
	cc := 0
	switch s[0] {
	case '+':
		signed = true
		cc++
	case '-':
		signed = true
		negative = true
		cc++
	}
	for ; cc < len(s) && isDecimalDigit(s[cc]); cc++ {
		if !overflow {
			acc = acc*10 + uint64(s[cc]-'0')
			if acc > math.MaxUint32 {
				overflow = true
			}
		}
	}
	var uValue uint32
	if !overflow {
		uValue = uint32(acc)
	}
	if !signed {
		return Number{kind: numberUnsigned, u: uValue}
	}
	limit := uint32(math.MaxInt32)
	if negative {
		limit++
	}
	if uValue > limit {
		uValue = 0
	}
	value := int32(uValue)
	if negative && value != int32Min {
		value = -value
	}
	return Number{kind: numberSigned, i: value}
}

// IsInteger reports whether the value was parsed as an integer (signed or
// unsigned), not a float.
func (n Number) IsInteger() bool {
	return n.kind == numberUnsigned || n.kind == numberSigned
}

// IsSigned reports whether the value carries a sign (signed int or float).
func (n Number) IsSigned() bool {
	return n.kind == numberSigned || n.kind == numberFloat
}

// GetSigned returns the value as int32, saturating from float.
func (n Number) GetSigned() int32 {
	switch n.kind {
	case numberUnsigned:
		return int32(n.u)
	case numberSigned:
		return n.i
	default:
		return saturatedFloatToInt32(n.f)
	}
}

// GetFloat returns the value as float32.
func (n Number) GetFloat() float32 {
	switch n.kind {
	case numberUnsigned:
		return float32(n.u)
	case numberSigned:
		return float32(n.i)
	default:
		return n.f
	}
}

// saturatedFloatToInt32 mirrors pdfium::saturated_cast<int32_t>(float).
func saturatedFloatToInt32(f float32) int32 {
	if math.IsNaN(float64(f)) {
		return 0
	}
	if f >= float32(int32Max) {
		return int32Max
	}
	if f <= float32(int32Min) {
		return int32Min
	}
	return int32(f)
}

// StringToFloat32 ports core/fxcrt/fx_string.cpp StringToFloat(ByteStringView):
// skip leading spaces, parse a leading float (general format, leading '+'
// allowed), and return 0 on parse error. Out-of-range parses keep the value.
func StringToFloat32(s string) float32 {
	start := 0
	for start < len(s) && s[start] == ' ' {
		start++
	}
	s = s[start:]

	end := floatPrefixLen(s)
	if end == 0 {
		return 0
	}
	value, err := strconv.ParseFloat(s[:end], 32)
	if err != nil {
		if numErr, ok := err.(*strconv.NumError); ok && numErr.Err == strconv.ErrRange {
			return float32(value)
		}
		return 0
	}
	return float32(value)
}

// floatPrefixLen returns the length of the longest leading substring of s that
// parses as a decimal float ([+-]?digits(.digits)?([eE][+-]?digits)?), matching
// fast_float's prefix behaviour for the number forms PDF uses.
func floatPrefixLen(s string) int {
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	digits := 0
	for i < len(s) && isDecimalDigit(s[i]) {
		i++
		digits++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && isDecimalDigit(s[i]) {
			i++
			digits++
		}
	}
	if digits == 0 {
		return 0
	}
	// Optional exponent; only consume it when well-formed.
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		expDigits := 0
		for j < len(s) && isDecimalDigit(s[j]) {
			j++
			expDigits++
		}
		if expDigits > 0 {
			i = j
		}
	}
	return i
}

func isDecimalDigit(b byte) bool { return b >= '0' && b <= '9' }
