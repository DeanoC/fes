package meshplace

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/meshcontent"
)

const (
	kitLiving = "kit-living"
	kitDen    = "kit-den"
	shellMac  = "shell-mac"
	emuOne    = "emu-one"
	emuTwo    = "emu-two"
)

func TestSingleFPGAKitIsSelected(t *testing.T) {
	shell := shellNode(shellMac)
	kit := fpgaNode(kitLiving, colecoABIs())
	// The shell is first. Order is not a rank, and a menu that cannot
	// run the title does not win because the menu is there.
	got := Place(fpgaEntry(), []Candidate{shell, kit}, Options{
		DisplayPreference: shellMac,
		LastDisplaySink:   shellMac,
	})
	mustSelected(t, got, kitLiving, true, true)
}

func TestEmptyFamilyListIsNotAnyRBF(t *testing.T) {
	// discovery.KitCapabilities advertises fpga_native with no ABI
	// families. That shape is not eligibility. Place does not read it.
	discoveryOnly := fpgaNode(kitLiving, nil)
	emptyDoc := fpgaNode(kitDen, []meshcontent.EligibleABI{})
	got := Place(fpgaEntry(), []Candidate{discoveryOnly, emptyDoc}, Options{})
	mustFail(t, got, ReasonNoCandidate)
}

func TestNodeDocumentABIFixtureSelectsMatchingKit(t *testing.T) {
	// Fixture bytes have the GET /v1/mesh/content/node shape: abis id
	// and major, optional packages. Place does not perform that GET.
	// The caller fills Candidate.ABIs from the document it already read.
	pkg := strings.Repeat("ab", 32)
	living := `{"node_id":"kit-living","abis":[{"id":"fes.simple-computer","major":2},{"id":"fes.application","major":1}],"packages":["` + pkg + `"]}`
	den := `{"node_id":"kit-den","abis":[]}`
	got := Place(fpgaEntry(), []Candidate{
		fpgaNode(kitDen, abisFromNodeDocument(t, den)),
		fpgaNode(kitLiving, abisFromNodeDocument(t, living)),
	}, Options{})
	mustSelected(t, got, kitLiving, true, true)
}

func TestWrongABIDoesNotSelect(t *testing.T) {
	wrongID := fpgaNode(kitLiving, []meshcontent.EligibleABI{{ID: "fes.simple-computer", Major: 1}})
	wrongMajor := fpgaNode(kitDen, []meshcontent.EligibleABI{{ID: "fes.application", Major: 2}})
	majorZero := fpgaNode("kit-zero", []meshcontent.EligibleABI{{ID: "fes.application", Major: 0}})
	got := Place(fpgaEntry(), []Candidate{wrongID, wrongMajor, majorZero}, Options{})
	mustFail(t, got, ReasonNoCandidate)
}

func TestSeveralFPGAKitsAreUnresolvedWithoutANamedSink(t *testing.T) {
	living := fpgaNode(kitLiving, colecoABIs())
	den := fpgaNode(kitDen, colecoABIs())
	forward := Place(fpgaEntry(), []Candidate{living, den}, Options{})
	reverse := Place(fpgaEntry(), []Candidate{den, living}, Options{})
	mustUnresolved(t, forward)
	if forward != reverse {
		t.Fatalf("order changed the result\n%+v\n%+v", forward, reverse)
	}
}

func TestSameNodeListedTwiceIsOneKit(t *testing.T) {
	kit := fpgaNode(kitLiving, colecoABIs())
	got := Place(fpgaEntry(), []Candidate{kit, kit}, Options{})
	mustSelected(t, got, kitLiving, true, true)
}

func TestDisplayPreferenceSelectsOneEligibleKit(t *testing.T) {
	living := fpgaNode(kitLiving, colecoABIs())
	den := fpgaNode(kitDen, colecoABIs())
	// Preference names the later kit. Last sink names the earlier one.
	got := Place(fpgaEntry(), []Candidate{living, den}, Options{
		DisplayPreference: kitDen,
		LastDisplaySink:   kitLiving,
	})
	mustSelected(t, got, kitDen, true, true)

	swapped := Place(fpgaEntry(), []Candidate{den, living}, Options{
		DisplayPreference: kitDen,
		LastDisplaySink:   kitLiving,
	})
	mustSelected(t, swapped, kitDen, true, true)
}

func TestLastDisplaySinkSelectsWhenPreferenceIsUnset(t *testing.T) {
	living := fpgaNode(kitLiving, colecoABIs())
	den := fpgaNode(kitDen, colecoABIs())
	got := Place(fpgaEntry(), []Candidate{living, den}, Options{LastDisplaySink: kitDen})
	mustSelected(t, got, kitDen, true, true)

	emptyPreference := Place(fpgaEntry(), []Candidate{den, living}, Options{
		DisplayPreference: "",
		LastDisplaySink:   kitLiving,
	})
	mustSelected(t, emptyPreference, kitLiving, true, true)
}

func TestPreferenceIgnoredWhenItIsNotADisplayThatCanExecute(t *testing.T) {
	living := fpgaNode(kitLiving, colecoABIs())
	den := fpgaNode(kitDen, colecoABIs())
	noSink := fpgaNode("kit-headless", colecoABIs())
	noSink.DisplaySink = false

	// The named preference cannot execute. Last sink can.
	shellPreference := Place(fpgaEntry(), []Candidate{shellNode(shellMac), living, den}, Options{
		DisplayPreference: shellMac,
		LastDisplaySink:   kitLiving,
	})
	mustSelected(t, shellPreference, kitLiving, true, true)

	// Preference names an eligible kit that does not advertise
	// DisplaySink. That name does not select it. Last sink does.
	headlessPreference := Place(fpgaEntry(), []Candidate{noSink, living, den}, Options{
		DisplayPreference: "kit-headless",
		LastDisplaySink:   kitDen,
	})
	mustSelected(t, headlessPreference, kitDen, true, true)

	// Neither name is a DisplaySink that can execute. Do not rank.
	neither := Place(fpgaEntry(), []Candidate{living, den, noSink}, Options{
		DisplayPreference: "kit-headless",
		LastDisplaySink:   shellMac,
	})
	mustUnresolved(t, neither)
}

func TestOneHeadlessKitStillSelectsExecute(t *testing.T) {
	kit := fpgaNode(kitLiving, colecoABIs())
	kit.DisplaySink = false
	kit.InputSource = false
	got := Place(fpgaEntry(), []Candidate{shellNode(shellMac), kit}, Options{
		DisplayPreference: shellMac,
	})
	mustSelected(t, got, kitLiving, false, false)
}

func TestDisplayAndInputStayOnTheExecuteNode(t *testing.T) {
	kit := fpgaNode(kitLiving, colecoABIs())
	kit.InputSource = false
	shell := shellNode(shellMac)
	got := Place(fpgaEntry(), []Candidate{shell, kit}, Options{})
	mustSelected(t, got, kitLiving, true, false)
	if got.Choice.DisplaySink == shellMac || got.Choice.InputSource == shellMac {
		t.Fatalf("picture or pad left the execute node: %+v", got.Choice)
	}
}

func TestNativeEmuSelectedOnlyWhenNoFPGACanRun(t *testing.T) {
	// The FPGA kit advertises fpga_native with an empty family list, so
	// it cannot run this title. The shell cannot either.
	got := Place(nativeEntry(), []Candidate{
		shellNode(shellMac),
		fpgaNode(kitLiving, nil),
		emuNode(emuOne),
	}, Options{DisplayPreference: shellMac})
	mustSelected(t, got, emuOne, true, true)
}

func TestFPGACandidateBeatsNativeEmu(t *testing.T) {
	got := Place(fpgaEntry(), []Candidate{
		emuNode(emuOne),
		fpgaNode(kitLiving, colecoABIs()),
	}, Options{DisplayPreference: emuOne})
	mustSelected(t, got, kitLiving, true, true)
}

func TestNativeEmuDoesNotRunAnFPGATitle(t *testing.T) {
	got := Place(fpgaEntry(), []Candidate{
		emuNode(emuOne),
		fpgaNode(kitLiving, nil),
	}, Options{})
	mustFail(t, got, ReasonNoCandidate)
}

func TestSeveralNativeEmuAreUnresolved(t *testing.T) {
	one := emuNode(emuOne)
	two := emuNode(emuTwo)
	forward := Place(nativeEntry(), []Candidate{one, two}, Options{
		DisplayPreference: emuOne,
		LastDisplaySink:   emuOne,
	})
	reverse := Place(nativeEntry(), []Candidate{two, one}, Options{
		DisplayPreference: emuTwo,
		LastDisplaySink:   emuTwo,
	})
	mustUnresolved(t, forward)
	mustUnresolved(t, reverse)
}

func TestNotLaunchableFailsClosed(t *testing.T) {
	browse := meshcontent.Entry{TitleID: "coleco-cart", System: "coleco", Launchable: false}
	got := Place(browse, []Candidate{fpgaNode(kitLiving, colecoABIs())}, Options{})
	mustFail(t, got, ReasonNotLaunchable)

	invalid := fpgaEntry()
	invalid.TitleID = "coleco/frogger"
	got = Place(invalid, []Candidate{fpgaNode(kitLiving, colecoABIs())}, Options{})
	mustFail(t, got, ReasonNotLaunchable)
}

func TestMeshMajorMismatchFailsClosed(t *testing.T) {
	bad := fpgaNode(kitLiving, colecoABIs())
	bad.MeshMajorOK = false
	// A native_emu node does not become the winner when the FPGA kit
	// that can run the title has a mesh-major mismatch.
	got := Place(fpgaEntry(), []Candidate{bad, shellNode(shellMac), emuNode(emuOne)}, Options{
		DisplayPreference: kitLiving,
	})
	mustFail(t, got, ReasonMeshMajor)

	other := fpgaNode(kitDen, colecoABIs())
	other.MeshMajorOK = false
	both := Place(fpgaEntry(), []Candidate{bad, other}, Options{DisplayPreference: kitLiving})
	mustFail(t, both, ReasonMeshMajor)

	emu := emuNode(emuOne)
	emu.MeshMajorOK = false
	native := Place(nativeEntry(), []Candidate{emu, fpgaNode(kitLiving, colecoABIs())}, Options{})
	mustFail(t, native, ReasonMeshMajor)
}

func TestMeshMajorDoesNotRankSeveralEligibleKits(t *testing.T) {
	good := fpgaNode(kitLiving, colecoABIs())
	bad := fpgaNode(kitDen, colecoABIs())
	bad.MeshMajorOK = false

	// Both kits can run the title. One mesh major mismatches. That
	// mismatch is not a rank, so the other kit is not the default winner.
	got := Place(fpgaEntry(), []Candidate{bad, good}, Options{})
	mustUnresolved(t, got)

	namedGood := Place(fpgaEntry(), []Candidate{bad, good}, Options{DisplayPreference: kitLiving})
	mustSelected(t, namedGood, kitLiving, true, true)

	// Naming the mismatched kit does not select it, and does not fall
	// through to the other kit.
	namedBad := Place(fpgaEntry(), []Candidate{good, bad}, Options{DisplayPreference: kitDen})
	mustUnresolved(t, namedBad)

	nativeGood := emuNode(emuOne)
	nativeBad := emuNode(emuTwo)
	nativeBad.MeshMajorOK = false
	native := Place(nativeEntry(), []Candidate{nativeBad, nativeGood}, Options{
		DisplayPreference: emuOne,
	})
	mustUnresolved(t, native)
}

func TestMissingRequiredSlotFailsClosed(t *testing.T) {
	got := Place(fpgaEntry(), []Candidate{fpgaNode(kitLiving, colecoABIs())}, Options{
		MissingRequiredSlot: true,
		DisplayPreference:   kitLiving,
	})
	mustFail(t, got, ReasonMissingSlot)

	native := Place(nativeEntry(), []Candidate{emuNode(emuOne)}, Options{MissingRequiredSlot: true})
	mustFail(t, native, ReasonMissingSlot)
}

func TestPlaceDoesNotMutateCandidates(t *testing.T) {
	candidates := []Candidate{
		fpgaNode(kitDen, colecoABIs()),
		fpgaNode(kitLiving, colecoABIs()),
	}
	before := append([]Candidate(nil), candidates...)
	_ = Place(fpgaEntry(), candidates, Options{DisplayPreference: kitLiving})
	if !reflect.DeepEqual(candidates, before) {
		t.Fatalf("candidates changed")
	}
}

func TestResultTypesHaveNoJSONTags(t *testing.T) {
	types := []reflect.Type{
		reflect.TypeOf(Result{}),
		reflect.TypeOf(Choice{}),
		reflect.TypeOf(Candidate{}),
		reflect.TypeOf(Options{}),
	}
	for _, typ := range types {
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if tag := field.Tag.Get("json"); tag != "" {
				t.Fatalf("%s.%s has json tag %q", typ.Name(), field.Name, tag)
			}
		}
	}
	optionNames := map[string]bool{}
	options := reflect.TypeOf(Options{})
	for i := 0; i < options.NumField(); i++ {
		optionNames[options.Field(i).Name] = true
	}
	if optionNames["Override"] || optionNames["OverrideNodeID"] {
		t.Fatal("slice 2 override is not part of this request")
	}
}

func mustSelected(t *testing.T, got Result, execute string, display, input bool) {
	t.Helper()
	if got.Outcome != OutcomeSelected || got.Reason != "" {
		t.Fatalf("outcome %q reason %q", got.Outcome, got.Reason)
	}
	want := Choice{Execute: execute}
	if display {
		want.DisplaySink = execute
	}
	if input {
		want.InputSource = execute
	}
	if got.Choice != want {
		t.Fatalf("choice %+v want %+v", got.Choice, want)
	}
}

func mustUnresolved(t *testing.T, got Result) {
	t.Helper()
	if got.Outcome != OutcomeUnresolved || got.Reason != "" || got.Choice != (Choice{}) {
		t.Fatalf("got %+v", got)
	}
}

func mustFail(t *testing.T, got Result, reason Reason) {
	t.Helper()
	if got.Outcome != OutcomeFailClosed || got.Reason != reason || got.Choice != (Choice{}) {
		t.Fatalf("got %+v want reason %q", got, reason)
	}
}

func fpgaEntry() meshcontent.Entry {
	return meshcontent.Entry{
		TitleID:    "coleco-frogger",
		System:     "coleco",
		Launchable: true,
		Execute:    []meshcontent.Execute{{Kind: meshcontent.ExecuteFPGANative}},
		Slots: []meshcontent.Slot{
			meshcontent.PackageSlot(meshcontent.PackageABI{
				PackageID: strings.Repeat("ab", 32),
				ABI:       "fes.application",
				Major:     1,
			}),
			meshcontent.PrimaryMediaSlot(meshcontent.SumSHA256([]byte("cart"))),
		},
	}
}

func nativeEntry() meshcontent.Entry {
	return meshcontent.Entry{
		TitleID:    "sms-alex",
		System:     "sms",
		Launchable: true,
		Execute:    []meshcontent.Execute{{Kind: meshcontent.ExecuteNativeEmu}},
		Slots: []meshcontent.Slot{
			meshcontent.PrimaryMediaSlot(meshcontent.SumSHA256([]byte("cart"))),
		},
	}
}

func colecoABIs() []meshcontent.EligibleABI {
	return []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}}
}

func fpgaNode(id string, abis []meshcontent.EligibleABI) Candidate {
	return Candidate{
		NodeID:      id,
		MeshMajorOK: true,
		Execute:     []string{meshcontent.ExecuteFPGANative},
		DisplaySink: true,
		InputSource: true,
		ABIs:        abis,
	}
}

func emuNode(id string) Candidate {
	return Candidate{
		NodeID:      id,
		MeshMajorOK: true,
		Execute:     []string{meshcontent.ExecuteNativeEmu},
		DisplaySink: true,
		InputSource: true,
	}
}

func shellNode(id string) Candidate {
	return Candidate{
		NodeID:      id,
		MeshMajorOK: true,
		DisplaySink: true,
		InputSource: true,
	}
}

// nodeDocument is the test double for GET /v1/mesh/content/node.
// Production types in this package do not carry these JSON tags.
type nodeDocument struct {
	NodeID   string    `json:"node_id"`
	ABIs     []nodeABI `json:"abis"`
	Packages []string  `json:"packages,omitempty"`
}

type nodeABI struct {
	ID    string `json:"id"`
	Major int    `json:"major"`
}

func abisFromNodeDocument(t *testing.T, raw string) []meshcontent.EligibleABI {
	t.Helper()
	var doc nodeDocument
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	out := make([]meshcontent.EligibleABI, 0, len(doc.ABIs))
	for _, abi := range doc.ABIs {
		out = append(out, meshcontent.EligibleABI{ID: abi.ID, Major: abi.Major})
	}
	return out
}
