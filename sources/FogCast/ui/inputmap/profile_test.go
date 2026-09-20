package inputmap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/remoteinput"
)

func TestIdentityPassThrough(t *testing.T) {
	t.Parallel()
	r, err := NewRemapper(Identity())
	if err != nil {
		t.Fatal(err)
	}
	a, _ := remoteinput.NormalizeGamepad("a", true)
	if got := r.Apply(a); got != a {
		t.Fatalf("identity mutated %+v -> %+v", a, got)
	}
	start, _ := remoteinput.NormalizeGamepad("start", true)
	if got := r.Apply(start); got != start {
		t.Fatalf("start mutated")
	}
	axis, _ := remoteinput.NormalizeAxis("left-x", 32767)
	if got := r.Apply(axis); got != axis {
		t.Fatalf("axis mutated")
	}
}

func TestBuiltinSwapAB(t *testing.T) {
	t.Parallel()
	p, ok := Builtin("swap-ab")
	if !ok || p.Name != NameSwapAB {
		t.Fatalf("builtin %+v %v", p, ok)
	}
	r, err := NewRemapper(p)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := remoteinput.NormalizeGamepad("a", true)
	b, _ := remoteinput.NormalizeGamepad("b", true)
	if got := r.Apply(a); got.Code != remoteinput.ButtonB {
		t.Fatalf("a -> %+v", got)
	}
	if got := r.Apply(b); got.Code != remoteinput.ButtonA {
		t.Fatalf("b -> %+v", got)
	}
	selectPress, _ := remoteinput.NormalizeGamepad("select", true)
	if got := r.Apply(selectPress); got.Code != remoteinput.ButtonSelect {
		t.Fatalf("select remapped %+v", got)
	}
}

func TestLoadJSONCustomRemap(t *testing.T) {
	t.Parallel()
	p, err := Load(filepath.Join("testdata", "b-launches.json"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "b-launches" {
		t.Fatalf("name %q", p.Name)
	}
	r, err := NewRemapper(p)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := remoteinput.NormalizeGamepad("b", true)
	if got := r.Apply(b); got.Code != remoteinput.ButtonA {
		t.Fatalf("b should launch: %+v", got)
	}
	a, _ := remoteinput.NormalizeGamepad("a", true)
	if got := r.Apply(a); got.Code != remoteinput.ButtonY {
		t.Fatalf("a should be y: %+v", got)
	}
}

func TestLoadNamedExampleProfile(t *testing.T) {
	t.Parallel()
	p, err := Load(filepath.Join("testdata", "swap-ab.json"))
	if err != nil {
		t.Fatal(err)
	}
	builtin, _ := Builtin("swap-ab")
	rFile, err := NewRemapper(p)
	if err != nil {
		t.Fatal(err)
	}
	rBuiltin, err := NewRemapper(builtin)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := remoteinput.NormalizeGamepad("a", true)
	if rFile.Apply(a).Code != rBuiltin.Apply(a).Code {
		t.Fatal("file and builtin swap-ab disagree")
	}
}

func TestPhysicalEvdevBinding(t *testing.T) {
	t.Parallel()
	p, err := Load(filepath.Join("testdata", "evdev-select.json"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRemapper(p)
	if err != nil {
		t.Fatal(err)
	}
	code, ok := r.PhysicalButton(304)
	if !ok || code != remoteinput.ButtonSelect {
		t.Fatalf("evdev 304 -> %d %v", code, ok)
	}
	if _, ok := r.PhysicalButton(305); ok {
		t.Fatal("unmapped evdev code")
	}
}

func TestResolveBuiltinAndFile(t *testing.T) {
	t.Parallel()
	p, err := Resolve("")
	if err != nil || p.Name != NameIdentity {
		t.Fatalf("empty %+v %v", p, err)
	}
	p, err = Resolve("identity")
	if err != nil || p.Name != NameIdentity {
		t.Fatalf("identity %+v %v", p, err)
	}
	p, err = Resolve(filepath.Join("testdata", "b-launches.json"))
	if err != nil || p.Name != "b-launches" {
		t.Fatalf("file %+v %v", p, err)
	}
}

func TestLoadRejectsCrossKindBinding(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "cross.json")
	if err := os.WriteFile(path, []byte(`{"name":"cross","bindings":{"a":"left-x"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "cannot bind") {
		t.Fatalf("err %v", err)
	}
}

func TestLoadRejectsPhysicalAxisTarget(t *testing.T) {
	t.Parallel()
	if _, err := NewRemapper(Profile{Name: "bad", Bindings: map[string]string{"evdev:304": "left-x"}}); err == nil {
		t.Fatal("physical axis target accepted")
	}
}

func TestLoadRejectsUnknownBinding(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(`{"name":"bad","bindings":{"a":"nope"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "unknown control") {
		t.Fatalf("err %v", err)
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "extra.json")
	if err := os.WriteFile(path, []byte(`{"name":"x","bindings":{},"extra":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestNilRemapperIsIdentity(t *testing.T) {
	t.Parallel()
	var r *Remapper
	a, _ := remoteinput.NormalizeGamepad("a", true)
	if r.Apply(a) != a {
		t.Fatal("nil remapper mutated")
	}
	if r.Profile().Name != NameIdentity {
		t.Fatalf("nil profile %q", r.Profile().Name)
	}
}
