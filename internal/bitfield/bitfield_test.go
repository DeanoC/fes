package bitfield

import "testing"

func TestParseAndMask(t *testing.T) {
	cases := []struct {
		spec  string
		mask  uint64
		shift uint64
	}{
		{"0", 0x1, 0},
		{"2:0", 0x7, 0},
		{"7:3", 0xf8, 3},
		{"7:6", 0xc0, 6},
		{"9", 0x200, 9},
		{"31:30", 0xc0000000, 30},
		{"31", 0x80000000, 31},
		{"11:0", 0xfff, 0},
	}
	for _, test := range cases {
		r, err := Parse(test.spec)
		if err != nil {
			t.Fatalf("Parse(%q): %v", test.spec, err)
		}
		if r.Mask() != test.mask {
			t.Fatalf("%q mask 0x%x want 0x%x", test.spec, r.Mask(), test.mask)
		}
		if r.Shift() != test.shift {
			t.Fatalf("%q shift %d want %d", test.spec, r.Shift(), test.shift)
		}
	}
}

func TestPlace(t *testing.T) {
	r, err := Parse("31:30")
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Place(1)
	if err != nil {
		t.Fatal(err)
	}
	if got != 0x40000000 {
		t.Fatalf("place 1 = 0x%x", got)
	}
	got, err = r.Place(2)
	if err != nil {
		t.Fatal(err)
	}
	if got != 0x80000000 {
		t.Fatalf("place 2 = 0x%x", got)
	}
}
