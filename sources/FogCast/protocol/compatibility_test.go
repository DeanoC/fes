package protocol

import "testing"

func TestCheckAPIVersion(t *testing.T) {
	for _, version := range []string{"", "v2", "V1", " v1", "v1 ", "v1.0", "v1"} {
		err := CheckAPIVersion(version)
		if (err == nil) != (version == "v1") {
			t.Fatalf("version %q: %v", version, err)
		}
	}
}
