package page

import "testing"

func TestTextCellIsBoldByFontName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		font string
		want bool
	}{
		{"Arial-Bold", "Arial-BoldMT", true},
		{"Helvetica-Bold", "Helvetica-Bold", true},
		{"Lora-SemiBold", "Lora-SemiBold", true},
		{"Poppins-Black", "Poppins-Black", true},
		{"Lora-Regular", "Lora-Regular", false},
		{"Poppins-Medium", "Poppins-Medium", false},
		{"empty", "", false},
		{"subword bold", "AAAAAA+Lora-Bold", true},
		{"subword regular", "AAAAAA+Lora-Regular", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := TextCell{FontName: tc.font}
			if got := c.IsBold(); got != tc.want {
				t.Errorf("IsBold(font=%q) = %v, want %v", tc.font, got, tc.want)
			}
		})
	}
}

func TestTextCellIsItalicByFontName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		font string
		want bool
	}{
		{"Arial-Italic", "Arial-ItalicMT", true},
		{"Helvetica-Oblique", "Helvetica-Oblique", true},
		{"Lora-Italic", "Lora-Italic", true},
		{"Lora-Regular", "Lora-Regular", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := TextCell{FontName: tc.font}
			if got := c.IsItalic(); got != tc.want {
				t.Errorf("IsItalic(font=%q) = %v, want %v", tc.font, got, tc.want)
			}
		})
	}
}

func TestTextCellIsBoldBySaneWeight(t *testing.T) {
	t.Parallel()
	// A sane weight ≥ 700 → bold, overriding a non-bold font name.
	c := TextCell{FontName: "Lora-Regular", FontWeight: 700}
	if !c.IsBold() {
		t.Error("weight 700 should be bold")
	}
	// Insane weight (uninitialized) must NOT trigger bold; name is the fallback.
	c = TextCell{FontName: "Lora-Regular", FontWeight: 1610612736}
	if c.IsBold() {
		t.Error("garbage weight should not make Lora-Regular bold")
	}
}

func TestTextCellIsItalicBySaneFlags(t *testing.T) {
	t.Parallel()
	// Italic flag bit (64) with sane flags → italic.
	c := TextCell{FontName: "Lora-Regular", FontFlags: FontFlagItalic}
	if !c.IsItalic() {
		t.Error("Italic flag set should be italic")
	}
	// Garbage flags must fall back to name.
	c = TextCell{FontName: "Lora-Regular", FontFlags: 524292}
	if c.IsItalic() {
		t.Error("garbage flags should not make Lora-Regular italic")
	}
}

func TestTextCellIsMonospace(t *testing.T) {
	t.Parallel()
	c := TextCell{FontName: "Courier", FontFlags: FontFlagFixedPitch}
	if !c.IsMonospace() {
		t.Error("FixedPitch flag should be monospace")
	}
	c = TextCell{FontName: "Lora-Regular", FontFlags: 0}
	if c.IsMonospace() {
		t.Error("non-fixed-pitch should not be monospace")
	}
}
