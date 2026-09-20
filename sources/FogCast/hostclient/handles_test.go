package hostclient

import (
	"strings"
	"testing"
)

func TestNormalizeHandle(t *testing.T) {
	t.Parallel()
	valid := strings.Repeat("ab", 32)
	upper := strings.Repeat("AB", 32)
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{name: "valid", value: valid, want: valid},
		{name: "uppercase", value: upper, want: valid},
		{name: "trimmed", value: "  " + upper + "\n", want: valid},
		{name: "empty", value: "", want: ""},
		{name: "short", value: "abc", want: ""},
		{name: "long", value: valid + "a", want: ""},
		{name: "non-hex", value: strings.Repeat("ag", 32), want: ""},
		{name: "whitespace-only", value: "  \t", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeHandle(tc.value); got != tc.want {
				t.Fatalf("NormalizeHandle(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}
