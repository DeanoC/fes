package misterruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

func staticProtocolFixture(t *testing.T, selected bool) (Protocol2Response, []byte) {
	t.Helper()
	root := filepath.Join("..", "..", "corepackage", "testdata", "core-bundle-v5")
	read := func(name string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	dir := t.TempDir()
	for name, source := range map[string]string{"manifest.toml": "manifests/valid-basic.toml", "core.rbf": "payloads/base.rbf", "persona.rbf": "payloads/persona.rbf"} {
		if err := os.WriteFile(filepath.Join(dir, name), read(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	inspection, archive, err := corepackage.ReadPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	var asset *expansion.Asset
	if selected {
		var m expansion.Manifest
		if err := json.Unmarshal(read("expansions/ram16k/manifest.json"), &m); err != nil {
			t.Fatal(err)
		}
		a, err := expansion.NewAsset(m, read("expansions/ram16k/cart.rbf"))
		if err != nil {
			t.Fatal(err)
		}
		asset = &a
	}
	bundle, err := corepackage.ComposeStaticArchive(archive, asset)
	if err != nil {
		t.Fatal(err)
	}
	var transport bytes.Buffer
	if err := bundle.Write(&transport); err != nil {
		t.Fatal(err)
	}
	var r Protocol2Response
	if err := json.Unmarshal([]byte(fixtureLines(t, "protocol-v2.jsonl")[5]), &r); err != nil {
		t.Fatal(err)
	}
	d := inspection.Descriptor
	abi := Protocol2Contract{ID: d.ABI.ID, Major: uint16(d.ABI.Major), Minor: uint16(d.ABI.Minor)}
	r.Core = &d.Core.ID
	r.ActivePackage = &Protocol2ActivePackage{PackageID: inspection.PackageID, Descriptor: d, StaticComposition: &bundle.Composition, PersistenceMode: "volatile", Observed: Protocol2Observed{ABI: &abi, BuildID: &d.Build.ID}}
	interfaces := []Protocol2Interface{{ID: "fes.keyboard.hid", Major: 1}, {ID: "fes.video.fixed-720p60", Major: 1}}
	r.Capabilities = Protocol2Capabilities{ProgrammingProfiles: []string{d.Target.ProgrammingProfile}, ABIs: []Protocol2ABI{{ID: abi.ID, Major: abi.Major, Minor: abi.Minor, Interfaces: interfaces}}, ActiveInterfaces: interfaces}
	return r, transport.Bytes()
}

func TestStaticProtocolStatusInspectionAndExactReceipt(t *testing.T) {
	for _, selected := range []bool{false, true} {
		r, _ := staticProtocolFixture(t, selected)
		encoded, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decodeProtocol2Response(encoded)
		if err != nil {
			t.Fatalf("static status rejected: %v shape=%v valid=%v active=%v caps=%v descriptor=%v; %s", err, validateProtocol2Shape(encoded), validProtocol2Response(r), validActivePackage(*r.ActivePackage, r.Capabilities), validCapabilities(r.Capabilities), corepackage.ValidateDescriptor(r.ActivePackage.Descriptor), encoded)
		}
		if !reflect.DeepEqual(decoded.ActivePackage.StaticComposition, r.ActivePackage.StaticComposition) {
			t.Fatal("lost two-image receipt")
		}
		staged := corepackage.Staged{PackageID: r.ActivePackage.PackageID, Descriptor: r.ActivePackage.Descriptor, StaticComposition: cloneStaticComposition(r.ActivePackage.StaticComposition)}
		if matchingAdoptedPackage([]corepackage.Staged{staged}, *r.ActivePackage) != 0 {
			t.Fatal("exact static restart receipt refused")
		}
		wrong := staged
		wrong.StaticComposition = cloneStaticComposition(staged.StaticComposition)
		wrong.StaticComposition.PersonaSHA256 = strings.Repeat("f", 64)
		if matchingAdoptedPackage([]corepackage.Staged{wrong}, *r.ActivePackage) >= 0 {
			t.Fatal("different persona adopted")
		}
		status := corePackageStatus(activationFromProtocol2(staged.PackageID, staged.Descriptor, r))
		if !reflect.DeepEqual(status.StaticComposition, staged.StaticComposition) || status.ExpansionSocket != "" {
			t.Fatal("public status lost selected receipt or claimed an empty CRAM socket")
		}
		for _, change := range []func(string) string{
			func(s string) string { return strings.Replace(s, `"base_size":12`, `"base_size":12.0`, 1) },
			func(s string) string { return strings.Replace(s, `"base_size":12`, `"base_size":12,"unknown":0`, 1) },
			func(s string) string { return strings.Replace(s, `"hardware_id":3221`, `"hardware_id":4321`, 1) },
			func(s string) string {
				return strings.Replace(s, `"control_build_id":"`+strings.Repeat("1", 32), `"control_build_id":"`+strings.Repeat("0", 32), 1)
			},
			func(s string) string {
				return strings.Replace(s, `"static_expansion":`, `"unknown_static_expansion":`, 1)
			},
		} {
			bad := change(string(encoded))
			if bad == string(encoded) {
				t.Fatal("negative control made no change")
			}
			if _, err := decodeProtocol2Response([]byte(bad)); err == nil {
				t.Fatal("accepted changed static response")
			}
		}
		r.ActivePackage.Composition = &expansion.Composition{}
		if _, err := json.Marshal(r); err == nil {
			t.Fatal("marshaled two composition forms")
		}
	}
	r, _ := staticProtocolFixture(t, false)
	var idle Protocol2Response
	if err := json.Unmarshal([]byte(fixtureLines(t, "protocol-v2.jsonl")[1]), &idle); err != nil {
		t.Fatal(err)
	}
	idle.InspectedPackage = &Protocol2Inspection{PackageID: r.ActivePackage.PackageID, Descriptor: r.ActivePackage.Descriptor, CompatibilityError: &Protocol2Error{Code: "unsupported_programming_profile", Phase: "compatibility", Message: "static profile not registered"}}
	encoded, err := json.Marshal(idle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeProtocol2Response(encoded); err != nil {
		t.Fatal("unregistered static inspection became unavailable", err)
	}
}

func TestStaticClientUsesExistingColdLoadAndChecksPersona(t *testing.T) {
	for _, selected := range []bool{false, true} {
		for _, wrong := range []bool{false, true} {
			response, _ := staticProtocolFixture(t, selected)
			c := *response.ActivePackage.StaticComposition
			if wrong {
				other := c
				other.PersonaSHA256 = strings.Repeat("f", 64)
				other.ID, _ = expansion.StaticCompositionID(other)
				response.ActivePackage.StaticComposition = &other
			}
			encoded, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			server := newSequenceSocketFixture(t, []string{fixtureLines(t, "protocol-v2.jsonl")[1] + "\n", string(encoded) + "\n"})
			path := ""
			if selected {
				path = "/partial"
			}
			_, err = NewClient(server.path).LoadStaticComposedCore(context.Background(), "/base", c.PackageID, path, c)
			if (err != nil) != wrong {
				t.Fatal("wrong static receipt outcome", err)
			}
			requests := server.wait(t)
			if len(requests) != 2 {
				t.Fatal(requests)
			}
			var wire map[string]json.RawMessage
			if err := json.Unmarshal([]byte(requests[1]), &wire); err != nil {
				t.Fatal(err)
			}
			if string(wire["operation"]) != `"load_composed_core"` || wire["payload_path"] != nil || (wire["expansion_path"] != nil) != selected {
				t.Fatal("new command or wrong image-path semantics", requests[1])
			}
		}
	}
}

type staticTargetControl struct {
	t               *testing.T
	active, current Protocol2Response
	calls           int
	incompatible    bool
	wrong           bool
	lost            bool
}

func (c *staticTargetControl) Protocol2Status(context.Context) (Protocol2Response, error) {
	return c.current, nil
}
func (c *staticTargetControl) Protocol2Stop(context.Context) (Protocol2Response, error) {
	var idle Protocol2Response
	err := json.Unmarshal([]byte(fixtureLines(c.t, "protocol-v2.jsonl")[1]), &idle)
	c.current = idle
	return idle, err
}
func (c *staticTargetControl) Protocol2LoadDevelopmentRBF(context.Context, string) (Protocol2Response, error) {
	return Protocol2Response{}, errors.New("unexpected raw load")
}
func (c *staticTargetControl) LoadCore(context.Context, string, string) (Protocol2Response, error) {
	return Protocol2Response{}, errors.New("static selection entered plain load")
}
func (c *staticTargetControl) InspectCore(_ context.Context, path, id string) (Protocol2Response, error) {
	inspection, err := corepackage.InspectPackage(path)
	if err != nil {
		return Protocol2Response{}, err
	}
	r := c.current
	r.InspectedPackage = &Protocol2Inspection{PackageID: inspection.PackageID, Descriptor: inspection.Descriptor, Compatible: !c.incompatible}
	if c.incompatible {
		r.InspectedPackage.CompatibilityError = &Protocol2Error{Code: "unsupported_programming_profile", Phase: "compatibility", Message: "static backend unavailable"}
	}
	return r, nil
}
func (c *staticTargetControl) LoadStaticComposedCore(_ context.Context, path, id, partial string, receipt expansion.StaticComposition) (Protocol2Response, error) {
	c.calls++
	if _, err := corepackage.InspectPackage(path); err != nil {
		c.t.Fatal(err)
	}
	if receipt.ExpansionID != "" {
		entries, err := os.ReadDir(partial)
		if err != nil || len(entries) != 2 {
			c.t.Fatal("partial directory not closed", err)
		}
	} else if partial != "" {
		c.t.Fatal("default selected an external partial")
	}
	r := c.active
	owned := *r.ActivePackage
	r.ActivePackage = &owned
	r.ActivePackage.StaticComposition = cloneStaticComposition(&receipt)
	if c.wrong {
		r.ActivePackage.StaticComposition.PersonaSHA256 = strings.Repeat("f", 64)
		r.ActivePackage.StaticComposition.ID, _ = expansion.StaticCompositionID(*r.ActivePackage.StaticComposition)
	}
	c.current = r
	if c.lost {
		return Protocol2Response{}, errors.New("static load reply lost")
	}
	return r, nil
}
func TestStaticTargetDispatchRestartStopAndUnregisteredRefusal(t *testing.T) {
	for _, selected := range []bool{false, true} {
		active, transport := staticProtocolFixture(t, selected)
		control := &staticTargetControl{t: t, active: active}
		control.Protocol2Stop(context.Background())
		root := t.TempDir()
		runtime := NewRuntime(control, "", 0, 0, WithCorePackageRoot(root))
		load := func() (CoreActivation, bool, *protocol.APIError) {
			return runtime.LoadComposedCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(transport)), bytes.NewReader(transport), active.ActivePackage.PackageID)
		}
		control.incompatible = true
		if _, attempted, err := load(); err == nil || err.Code != protocol.CodeUnsupportedOperation || attempted || control.calls != 0 {
			t.Fatal("unregistered profile reached mutation", err)
		}
		control.incompatible = false
		activation, attempted, err := load()
		if err != nil || !attempted || control.calls != 1 || !reflect.DeepEqual(activation.StaticComposition, active.ActivePackage.StaticComposition) {
			t.Fatal("static dispatch", activation, attempted, err)
		}
		if _, attempted, err := load(); err == nil || err.Code != protocol.CodeBusy || attempted || control.calls != 1 {
			t.Fatal("changed static setup without Stop", err)
		}
		restarted := NewRuntime(control, "", 0, 0, WithCorePackageRoot(root))
		status := restarted.Reconcile(context.Background())
		if status.CorePackage == nil || !reflect.DeepEqual(status.CorePackage.StaticComposition, activation.StaticComposition) {
			t.Fatal("static restart lost receipt", status)
		}
		if _, err := restarted.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
		entries, err2 := os.ReadDir(root)
		if err2 != nil || len(entries) != 0 {
			t.Fatal("static Stop left package ownership", err2)
		}
	}
}

func TestStaticLostReplyRequiresExactPersonaAndRetainsUncertainInputs(t *testing.T) {
	for _, mode := range []string{"lost-exact", "lost-other", "reply-other"} {
		t.Run(mode, func(t *testing.T) {
			active, transport := staticProtocolFixture(t, true)
			control := &staticTargetControl{t: t, active: active, wrong: mode != "lost-exact", lost: mode != "reply-other"}
			control.Protocol2Stop(context.Background())
			root := t.TempDir()
			runtime := NewRuntime(control, "", 0, 0, WithCorePackageRoot(root))
			defer runtime.Stop(context.Background())
			activation, attempted, err := runtime.LoadComposedCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(transport)), bytes.NewReader(transport), active.ActivePackage.PackageID)
			if !attempted || control.calls != 1 {
				t.Fatal("load replay or mutation boundary lost", attempted, control.calls)
			}
			if mode == "lost-exact" {
				if err != nil || !reflect.DeepEqual(activation.StaticComposition, active.ActivePackage.StaticComposition) {
					t.Fatal("exact committed persona was not recovered", err)
				}
			} else {
				if err == nil || len(runtime.retiredPackages) != 1 {
					t.Fatal("uncertain persona was accepted or sources dropped", err)
				}
				retained, adoptErr := corepackage.Adopt(root)
				if adoptErr != nil || len(retained) != 1 || retained[0].StaticComposition.ID != active.ActivePackage.StaticComposition.ID {
					t.Fatal("uncertain load discarded its exact sources", adoptErr)
				}
			}
			changed := control.current
			owned := *changed.ActivePackage
			owned.StaticComposition = cloneStaticComposition(owned.StaticComposition)
			owned.StaticComposition.ID = strings.Repeat("f", 64)
			changed.ActivePackage = &owned
			if sameProtocol2RuntimeState(control.current, changed) {
				t.Fatal("state comparison ignored changed static receipt")
			}
		})
	}
}

func TestStaticTargetRejectsPlainAndInitializedInputsBeforeMutation(t *testing.T) {
	active, transport := staticProtocolFixture(t, false)
	bundle, err := corepackage.ReadStaticCompositionBundle(bytes.NewReader(transport))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"plain", "initialized-plain", "initialized-composed"} {
		t.Run(mode, func(t *testing.T) {
			body := bundle.Package
			composed := false
			if mode != "plain" {
				input := corepackage.RomInit{Package: bundle.Package, Programmed: []byte("not a static persona"), ImageSHA256: strings.Repeat("a", 64)}
				if mode == "initialized-composed" {
					input.Composition = transport
					composed = true
				}
				var err error
				body, err = corepackage.WriteRomInit(input)
				if err != nil {
					t.Fatal(err)
				}
			}
			control := &staticTargetControl{t: t, active: active}
			control.Protocol2Stop(context.Background())
			root := t.TempDir()
			runtime := NewRuntime(control, "", 0, 0, WithCorePackageRoot(root))
			var attempted bool
			var failure *protocol.APIError
			if composed {
				_, attempted, failure = runtime.LoadComposedCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(body)), bytes.NewReader(body), active.ActivePackage.PackageID)
			} else {
				_, attempted, failure = runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(body)), bytes.NewReader(body))
			}
			if failure == nil || failure.Code != protocol.CodeInvalidArchive || attempted || control.calls != 0 {
				t.Fatal("static base accepted without its cold selection", attempted, failure)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatal("rejected static input left publications", err)
			}
		})
	}
}
