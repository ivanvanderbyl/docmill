package font

import "testing"

func TestStripSubsetTag(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"AAAAAA+Lora-Regular", "Lora-Regular"},
		{"EAAAAA+Poppins-SemiBold", "Poppins-SemiBold"},
		{"Helvetica", "Helvetica"},     // no subset tag
		{"ABC+Short", "ABC+Short"},     // tag too short (<6) → unchanged
		{"ABCDEF+Name", "Name"},        // exactly 6
		{"abcdef+Name", "abcdef+Name"}, // lowercase tag → not a subset tag
		{"", ""},
	}
	for _, c := range cases {
		if got := stripSubsetTag(c.in); got != c.want {
			t.Errorf("stripSubsetTag(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFontWeightByName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		want int
	}{
		{"Arial-Bold", 700},
		{"Helvetica-Black", 900},
		{"Lora-SemiBold", 700},
		{"Poppins-Medium", 500},
		{"Lora-Light", 200},
		{"Lora-Regular", 0},
		{"Helvetica", 0},
		{"", 0},
	}
	for _, c := range cases {
		f := &Font{baseFontName: c.name}
		if got := f.FontWeight(); got != c.want {
			t.Errorf("FontWeight(%q) = %d, want %d", c.name, got, c.want)
		}
	}
}
