package appliance

import (
	"strings"
	"testing"
)

func validJSON() string {
	return `{"format":1,"board":"de10-nano","boot_abi":"fes-bootstrap-v1","version":"v1","kernel_sha256":"` + strings.Repeat("a", 64) + `","image_sha256":"` + strings.Repeat("b", 64) + `","image_size":4096,"fes_revision":"` + strings.Repeat("c", 40) + `","fogcast_revision":"` + strings.Repeat("d", 40) + `","runtime_revision":"` + strings.Repeat("e", 40) + `"}`
}
func TestDecodeManifestClosedSchema(t *testing.T) {
	if _, e := DecodeManifest(strings.NewReader(validJSON())); e != nil {
		t.Fatal(e)
	}
	for _, input := range []string{strings.Replace(validJSON(), `"format":1`, `"format":1,"format":1`, 1), strings.Replace(validJSON(), `"format":1`, `"FORMAT":1`, 1), strings.Replace(validJSON(), `"format":1`, `"format":1,"extra":1`, 1), validJSON() + `{}`, strings.Replace(validJSON(), `"image_size":4096`, `"image_size":0`, 1), strings.Replace(validJSON(), `"version":"v1"`, `"version":null`, 1), strings.Replace(validJSON(), `de10-nano`, `wrong-board`, 1)} {
		if _, e := DecodeManifest(strings.NewReader(input)); e == nil {
			t.Fatalf("accepted invalid manifest %s", input)
		}
	}
}
