package appliance

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
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

func TestOptionalImagePassesStaysInManifestDigest(t *testing.T) {
	legacy, e := DecodeManifest(strings.NewReader(validJSON()))
	if e != nil || legacy.ImagePasses != 0 {
		t.Fatal(e, legacy.ImagePasses)
	}
	raw, e := json.Marshal(legacy)
	if e != nil || bytes.Contains(raw, []byte("image_passes")) {
		t.Fatalf("two-pass remarshal %s %v", raw, e)
	}
	withPasses := func(value string) string {
		return strings.Replace(validJSON(), `"format":1`, `"format":1,"image_passes":`+value, 1)
	}
	m, e := DecodeManifest(strings.NewReader(withPasses("1")))
	if e != nil || m.ImagePasses != 1 {
		t.Fatal(e, m.ImagePasses)
	}
	raw, e = json.Marshal(m)
	if e != nil || !bytes.Contains(raw, []byte(`"image_passes":1`)) {
		t.Fatalf("remarshal dropped image_passes: %s %v", raw, e)
	}
	again, e := DecodeManifest(bytes.NewReader(raw))
	if e != nil || again != m {
		t.Fatalf("round trip %+v %v", again, e)
	}
	sum := sha256.Sum256(raw)
	stripped := bytes.ReplaceAll(raw, []byte(`,"image_passes":1`), nil)
	if bytes.Equal(raw, stripped) || sha256.Sum256(stripped) == sum {
		t.Fatal("image_passes is outside the remarshaled manifest digest")
	}
	positive, e := DecodeManifest(strings.NewReader(withPasses("2")))
	if e != nil || positive.ImagePasses != 2 {
		t.Fatal(e, positive.ImagePasses)
	}
	maxInt := "9223372036854775807"
	wide, e := DecodeManifest(strings.NewReader(withPasses(maxInt)))
	if e != nil || wide.ImagePasses != 9223372036854775807 {
		t.Fatal(e, wide.ImagePasses)
	}
	for _, bad := range []string{"0", "-1", "1.5", "01", `"1"`, "true", "null", "2e0", "9223372036854775808"} {
		if _, e = DecodeManifest(strings.NewReader(withPasses(bad))); e == nil {
			t.Fatalf("accepted image_passes %s", bad)
		}
	}
	duplicate := strings.Replace(withPasses("1"), `"image_passes":1`, `"image_passes":1,"image_passes":1`, 1)
	if _, e = DecodeManifest(strings.NewReader(duplicate)); e == nil {
		t.Fatal("accepted duplicate image_passes")
	}
	unknown := strings.Replace(withPasses("1"), `"image_passes":1`, `"image_passes":1,"extra":1`, 1)
	if _, e = DecodeManifest(strings.NewReader(unknown)); e == nil {
		t.Fatal("accepted unknown field beside image_passes")
	}
}
