package hexnum

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want uint64
	}{
		{"0xff706000", 0xff706000},
		{"0x0", 0},
		{"7", 7},
		{"  0x3fff  ", 0x3fff},
	}
	for _, test := range cases {
		got, err := Parse(test.in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", test.in, err)
		}
		if got != test.want {
			t.Fatalf("Parse(%q)=0x%x want 0x%x", test.in, got, test.want)
		}
	}
}
