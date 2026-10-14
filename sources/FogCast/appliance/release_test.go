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
	if _, _, e := DecodeManifest(strings.NewReader(validJSON())); e != nil {
		t.Fatal(e)
	}
	for _, input := range []string{strings.Replace(validJSON(), `"format":1`, `"format":1,"format":1`, 1), strings.Replace(validJSON(), `"format":1`, `"FORMAT":1`, 1), strings.Replace(validJSON(), `"format":1`, `"format":1,"extra":1`, 1), validJSON() + `{}`, strings.Replace(validJSON(), `"image_size":4096`, `"image_size":0`, 1), strings.Replace(validJSON(), `"version":"v1"`, `"version":null`, 1), strings.Replace(validJSON(), `de10-nano`, `wrong-board`, 1)} {
		if _, _, e := DecodeManifest(strings.NewReader(input)); e == nil {
			t.Fatalf("accepted invalid manifest %s", input)
		}
	}
}

func TestOptionalImagePassesStaysInManifestDigest(t *testing.T) {
	legacy, _, e := DecodeManifest(strings.NewReader(validJSON()))
	if e != nil || legacy.ImagePasses != 0 {
		t.Fatal(e, legacy.ImagePasses)
	}
	raw, e := json.Marshal(legacy)
	if e != nil || bytes.Contains(raw, []byte("image_passes")) || bytes.Contains(raw, []byte("manifest_format")) {
		t.Fatalf("two-pass remarshal %s %v", raw, e)
	}
	withPasses := func(value string) string {
		return strings.Replace(validJSON(), `"format":1`, `"format":1,"image_passes":`+value, 1)
	}
	m, _, e := DecodeManifest(strings.NewReader(withPasses("1")))
	if e != nil || m.ImagePasses != 1 {
		t.Fatal(e, m.ImagePasses)
	}
	raw, e = json.Marshal(m)
	if e != nil || !bytes.Contains(raw, []byte(`"image_passes":1`)) {
		t.Fatalf("remarshal dropped image_passes: %s %v", raw, e)
	}
	again, _, e := DecodeManifest(bytes.NewReader(raw))
	if e != nil || again != m {
		t.Fatalf("round trip %+v %v", again, e)
	}
	sum := sha256.Sum256(raw)
	stripped := bytes.ReplaceAll(raw, []byte(`,"image_passes":1`), nil)
	if bytes.Equal(raw, stripped) || sha256.Sum256(stripped) == sum {
		t.Fatal("image_passes is outside the remarshaled manifest digest")
	}
	positive, _, e := DecodeManifest(strings.NewReader(withPasses("2")))
	if e != nil || positive.ImagePasses != 2 {
		t.Fatal(e, positive.ImagePasses)
	}
	maxInt := "9223372036854775807"
	wide, _, e := DecodeManifest(strings.NewReader(withPasses(maxInt)))
	if e != nil || wide.ImagePasses != 9223372036854775807 {
		t.Fatal(e, wide.ImagePasses)
	}
	for _, bad := range []string{"0", "-1", "1.5", "01", `"1"`, "true", "null", "2e0", "9223372036854775808"} {
		if _, _, e = DecodeManifest(strings.NewReader(withPasses(bad))); e == nil {
			t.Fatalf("accepted image_passes %s", bad)
		}
	}
	duplicate := strings.Replace(withPasses("1"), `"image_passes":1`, `"image_passes":1,"image_passes":1`, 1)
	if _, _, e = DecodeManifest(strings.NewReader(duplicate)); e == nil {
		t.Fatal("accepted duplicate image_passes")
	}
	unknown := strings.Replace(withPasses("1"), `"image_passes":1`, `"image_passes":1,"extra":1`, 1)
	if _, _, e = DecodeManifest(strings.NewReader(unknown)); e == nil {
		t.Fatal("accepted unknown field beside image_passes")
	}
}

func withManifestFormat(format, extra string) string {
	insert := `"manifest_format":"` + format + `"`
	if extra != "" {
		insert += "," + extra
	}
	return strings.Replace(validJSON(), `"format":1`, `"format":1,`+insert, 1)
}

func TestManifestFormatForwardsOptionalFields(t *testing.T) {
	m, ignored, e := DecodeManifest(strings.NewReader(withManifestFormat("1.1", `"future_field":{"keep":true}`)))
	if e != nil || m.ManifestFormat != "1.1" || m.Version != "v1" || len(ignored) != 1 || ignored[0] != "future_field" {
		t.Fatalf("higher minor: %+v ignored %v err %v", m, ignored, e)
	}
	raw, e := json.Marshal(m)
	if e != nil || bytes.Contains(raw, []byte("future_field")) || !bytes.Contains(raw, []byte(`"manifest_format":"1.1"`)) {
		t.Fatalf("ignored field survived remarshal: %s %v", raw, e)
	}
	same, ignored, e := DecodeManifest(strings.NewReader(withManifestFormat("1.0", `"future_field":1`)))
	if e == nil || len(ignored) != 0 || same.Version != "" {
		t.Fatalf("same minor accepted unknown field: %+v %v", same, ignored)
	}
	for _, bad := range []string{"2.0", "2.1", "0.1"} {
		if _, _, e = DecodeManifest(strings.NewReader(withManifestFormat(bad, `"future_field":1`))); e == nil {
			t.Fatalf("accepted unknown major %s", bad)
		}
	}
	missing := strings.Replace(withManifestFormat("1.1", `"future_field":1`), `"version":"v1",`, "", 1)
	if _, _, e = DecodeManifest(strings.NewReader(missing)); e == nil {
		t.Fatal("accepted missing version on a higher minor")
	}
	malformed := strings.Replace(withManifestFormat("1.1", `"future_field":1`), `"image_size":4096`, `"image_size":"4096"`, 1)
	if _, _, e = DecodeManifest(strings.NewReader(malformed)); e == nil {
		t.Fatal("accepted malformed image_size on a higher minor")
	}
	for _, bad := range []string{"1", "1.0.0", "01.1", "1.01", ""} {
		input := strings.Replace(validJSON(), `"format":1`, `"format":1,"manifest_format":"`+bad+`"`, 1)
		if bad == "" {
			input = strings.Replace(validJSON(), `"format":1`, `"format":1,"manifest_format":1.1`, 1)
		}
		if _, _, e = DecodeManifest(strings.NewReader(input)); e == nil {
			t.Fatalf("accepted malformed manifest_format %q", bad)
		}
	}
	duplicate := strings.Replace(withManifestFormat("1.1", `"future_field":1`), `"future_field":1`, `"future_field":1,"future_field":2`, 1)
	if _, _, e = DecodeManifest(strings.NewReader(duplicate)); e == nil {
		t.Fatal("accepted duplicate unknown field")
	}
}

func TestHigherMinorCaseVariantDoesNotOverwrite(t *testing.T) {
	// The case variant follows the recognized field. encoding/json keeps the
	// last case-insensitive match, so this is the order that used to replace
	// a value the exact-name scan had already accepted.
	input := strings.TrimSuffix(withManifestFormat("1.1", `"image_passes":2`), "}") + `,"Version":"v2","Image_Passes":0}`
	m, ignored, e := DecodeManifest(strings.NewReader(input))
	if e != nil || m.Version != "v1" || m.ImagePasses != 2 || m.ManifestFormat != "1.1" {
		t.Fatalf("case variant overwrote manifest: %+v ignored %v err %v", m, ignored, e)
	}
	if len(ignored) != 2 || ignored[0] != "Version" || ignored[1] != "Image_Passes" {
		t.Fatalf("ignored %v", ignored)
	}
	raw, e := json.Marshal(m)
	if e != nil || !bytes.Contains(raw, []byte(`"version":"v1"`)) || !bytes.Contains(raw, []byte(`"image_passes":2`)) || bytes.Contains(raw, []byte(`"v2"`)) || bytes.Contains(raw, []byte("Image_Passes")) {
		t.Fatalf("remarshal kept the case variant: %s %v", raw, e)
	}
	closed := strings.TrimSuffix(validJSON(), "}") + `,"Version":"v2"}`
	if _, _, e = DecodeManifest(strings.NewReader(closed)); e == nil {
		t.Fatal("accepted a case variant on manifest format 1.0")
	}
	onlyVariant := strings.Replace(withManifestFormat("1.1", ""), `"version":"v1"`, `"Version":"v2"`, 1)
	if _, _, e = DecodeManifest(strings.NewReader(onlyVariant)); e == nil {
		t.Fatal("accepted Version in place of version")
	}
}
