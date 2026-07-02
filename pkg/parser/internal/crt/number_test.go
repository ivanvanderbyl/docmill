// Ported from core/fxcrt/fx_number_unittest.cpp @ pdfium 0db284a42.
// Expected values are PDFium's own; see plan 009 Phase A (validation tier 1).
package crt

import (
	"math"
	"testing"
)

func TestNumberDefault(t *testing.T) {
	var n Number
	if !n.IsInteger() {
		t.Error("default Number should be an integer")
	}
	if n.IsSigned() {
		t.Error("default Number should be unsigned")
	}
	if n.GetSigned() != 0 {
		t.Errorf("GetSigned = %d, want 0", n.GetSigned())
	}
	if n.GetFloat() != 0.0 {
		t.Errorf("GetFloat = %v, want 0", n.GetFloat())
	}
}

func TestNumberFromSigned(t *testing.T) {
	n := NumberFromInt(-128)
	if !n.IsInteger() || !n.IsSigned() {
		t.Fatal("expected signed integer")
	}
	if n.GetSigned() != -128 {
		t.Errorf("GetSigned = %d, want -128", n.GetSigned())
	}
	if n.GetFloat() != -128.0 {
		t.Errorf("GetFloat = %v, want -128", n.GetFloat())
	}
}

func TestNumberFromFloat(t *testing.T) {
	n := NumberFromFloat(-100.001)
	if n.IsInteger() {
		t.Error("float Number should not be an integer")
	}
	if !n.IsSigned() {
		t.Error("float Number should be signed")
	}
	if n.GetSigned() != -100 {
		t.Errorf("GetSigned = %d, want -100", n.GetSigned())
	}
	if n.GetFloat() != -100.001 {
		t.Errorf("GetFloat = %v, want -100.001", n.GetFloat())
	}

	// Positive saturation.
	n3 := NumberFromFloat(1e17)
	if n3.IsInteger() || !n3.IsSigned() {
		t.Error("1e17 should be a signed float")
	}
	if n3.GetSigned() != math.MaxInt32 {
		t.Errorf("GetSigned = %d, want MaxInt32", n3.GetSigned())
	}

	// Negative saturation.
	n4 := NumberFromFloat(-1e17)
	if n4.GetSigned() != math.MinInt32 {
		t.Errorf("GetSigned = %d, want MinInt32", n4.GetSigned())
	}
}

func TestNumberFromStringUnsigned(t *testing.T) {
	check := func(input string, want int32) {
		n := NumberFromString(input)
		if !n.IsInteger() {
			t.Errorf("%q: expected integer", input)
		}
		if n.IsSigned() {
			t.Errorf("%q: expected unsigned", input)
		}
		if n.GetSigned() != want {
			t.Errorf("%q: GetSigned = %d, want %d", input, n.GetSigned(), want)
		}
	}
	for _, tc := range []struct {
		in   string
		want int32
	}{
		{"", 0}, {"0", 0}, {"10", 10},
		// Overflow resets to 0 (FX_SAFE_UINT32 invalid -> ValueOrDefault(0)).
		{"4223423494965252", 0}, {"4294967296", 0}, {"4294967297", 0}, {"5000000000", 0},
		// No explicit sign lets the value wrap negative when read as signed.
		{"4294965252", -2044}, {"4294967247", -49}, {"4294967248", -48},
		{"4294967292", -4}, {"4294967295", -1},
	} {
		check(tc.in, tc.want)
	}
}

func TestNumberFromStringSigned(t *testing.T) {
	for _, tc := range []struct {
		in       string
		wantInt  bool
		wantSign bool
		want     int32
	}{
		{"-0", true, true, 0},
		{"+0", true, true, 0},
		{"-10", true, true, -10},
		{"+10", true, true, 10},
		{"-2147483648", true, true, math.MinInt32},
		{"+2147483647", true, true, math.MaxInt32},
		{"-2147483649", false, false, 0}, // underflow -> 0
		{"+2147483648", false, false, 0}, // overflow -> 0
	} {
		n := NumberFromString(tc.in)
		if tc.in == "-2147483648" || tc.in == "+2147483647" || tc.in == "-0" ||
			tc.in == "+0" || tc.in == "-10" || tc.in == "+10" {
			if !n.IsInteger() || !n.IsSigned() {
				t.Errorf("%q: expected signed integer", tc.in)
			}
		}
		if n.GetSigned() != tc.want {
			t.Errorf("%q: GetSigned = %d, want %d", tc.in, n.GetSigned(), tc.want)
		}
	}
}

func TestNumberFromStringFloat(t *testing.T) {
	n := NumberFromString("3.24")
	if got := n.GetFloat(); got != float32(3.24) {
		t.Errorf("GetFloat = %v, want 3.24", got)
	}
}
