package misterruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

type rejectedPartsControl struct {
	*Client
	calls int
}

func (c *rejectedPartsControl) LoadPartsCore(context.Context, string, string, string, []PartPath, expansion.PartsComposition) (Protocol2Response, error) {
	c.calls++
	return Protocol2Response{}, errInvalidRuntimeRequest
}

func (c *rejectedPartsControl) InspectPartsCore(context.Context, string, string, string, []PartPath, expansion.PartsComposition) (Protocol2Response, error) {
	c.calls++
	return Protocol2Response{}, errInvalidRuntimeRequest
}

func TestPartsLoadRejectsLegacyROMEnvelopeBeforePublicationOrDispatch(t *testing.T) {
	wrapped, err := corepackage.WriteRomInit(corepackage.RomInit{
		Package: []byte("sealed base"), Composition: []byte("nested parts transport"),
		Programmed: []byte("alternate programmed artifact"), ImageSHA256: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := corepackage.ReadRomInit(wrapped); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	control := &rejectedPartsControl{Client: NewClient(root + "/absent.sock")}
	runtime := &Runtime{control: control, corePackageRoot: root}
	ctx := context.Background()
	_, attempted, failure := runtime.LoadPartsCoreOwned(ctx, ctx, ctx, int64(len(wrapped)), bytes.NewReader(wrapped))
	if attempted || failure == nil || failure.Code != protocol.CodeInvalidArchive || failure.Message != "developer parts require a parts archive" {
		t.Fatalf("attempted=%v failure=%#v", attempted, failure)
	}
	if _, failure := runtime.InspectPartsCore(ctx, int64(len(wrapped)), bytes.NewReader(wrapped)); failure == nil || failure.Code != protocol.CodeInvalidArchive {
		t.Fatalf("inspection failure=%#v", failure)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 || control.calls != 0 {
		t.Fatalf("publications=%v calls=%d error=%v", entries, control.calls, err)
	}
}

func partsResponse(t *testing.T) (Protocol2Response, expansion.PartsComposition) {
	return partsResponseLayout(t, expansion.ColecoVideoLayout)
}

func partsResponseLayout(t *testing.T, layout string) (Protocol2Response, expansion.PartsComposition) {
	t.Helper()
	r, _ := compositionResponse(t)
	a := r.ActivePackage
	a.Composition = nil
	a.Descriptor.Format = 2
	a.Descriptor.Core.ID = "fes.coleco"
	a.Descriptor.ABI.ID = "fes.application"
	a.Observed.ABI.ID = "fes.application"
	a.PersistenceMode = "volatile"
	r.Capabilities.ABIs[0].ID = "fes.application"
	a.Descriptor.Interfaces[2] = corepackage.Interface{ID: expansion.ColecoSlot, Major: 2}
	a.Descriptor.Interfaces = append(a.Descriptor.Interfaces, corepackage.Interface{ID: expansion.VideoSlot, Major: 1})
	if layout == expansion.ColecoNativeVideoLayout {
		a.Descriptor.Interfaces[len(a.Descriptor.Interfaces)-1].ID = expansion.NativeVideoSlot
	}
	c := expansion.PartsComposition{PackageID: a.PackageID, Layout: layout, Parts: []expansion.PartSelection{{Role: "video", PartID: strings.Repeat("c", 64)}}, ShellSHA256: a.Descriptor.Payload.SHA256, PayloadSHA256: strings.Repeat("d", 64), PayloadSize: 40408}
	c.ID, _ = expansion.PartsCompositionID(c.PackageID, c.Layout, c.Parts, c.PayloadSHA256)
	a.PartsComposition = clonePartsComposition(&c)
	return r, c
}

func TestNativePartsStatusBindsExactSourceMarkerAndClosedLayout(t *testing.T) {
	r, c := partsResponseLayout(t, expansion.ColecoNativeVideoLayout)
	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeProtocol2Response(encoded); err != nil {
		t.Fatalf("valid native parts: %v", err)
	}
	for name, change := range map[string]func(*Protocol2Response){
		"raster receipt": func(r *Protocol2Response) {
			c := r.ActivePackage.PartsComposition
			c.Layout = expansion.ColecoVideoLayout
			c.ID, _ = expansion.PartsCompositionID(c.PackageID, c.Layout, c.Parts, c.PayloadSHA256)
		},
		"raster descriptor": func(r *Protocol2Response) { r.ActivePackage.Descriptor.Interfaces[3].ID = expansion.VideoSlot },
		"both source markers": func(r *Protocol2Response) {
			r.ActivePackage.Descriptor.Interfaces = append(r.ActivePackage.Descriptor.Interfaces, corepackage.Interface{ID: expansion.VideoSlot, Major: 1})
		},
		"unknown geometry version": func(r *Protocol2Response) { r.ActivePackage.Descriptor.Interfaces[3].Major = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			var bad Protocol2Response
			if err := json.Unmarshal(encoded, &bad); err != nil {
				t.Fatal(err)
			}
			change(&bad)
			if validProtocol2Response(bad) {
				t.Fatal("accepted source marker and receipt mismatch")
			}
		})
	}
	socket := newSequenceSocketFixture(t, []string{fixtureLines(t, "protocol-v2.jsonl")[1] + "\n", string(encoded) + "\n"})
	response, err := NewClient(socket.path).LoadPartsCore(context.Background(), "/base", c.PackageID, "/linked/linked.rbf", []PartPath{{"video", "/video"}}, c)
	if err != nil || !reflect.DeepEqual(response.ActivePackage.PartsComposition, &c) {
		t.Fatalf("native load through existing operation: %+v %v", response, err)
	}
	staged := corepackage.Staged{PackageID: c.PackageID, Descriptor: r.ActivePackage.Descriptor, PartsComposition: &c}
	runtime := &Runtime{}
	_, disposition := runtime.observeLostCoreLoad(context.Background(), compositionStatusControl{r}, staged, Protocol2Response{State: "idle"})
	if disposition != coreLoadConfirmed || matchingAdoptedPackage([]corepackage.Staged{staged}, *r.ActivePackage) != 0 {
		t.Fatal("native lost response or adoption did not retain exact tuple")
	}
	wrong := clonePartsComposition(&c)
	wrong.Layout = expansion.ColecoVideoLayout
	wrong.ID, _ = expansion.PartsCompositionID(wrong.PackageID, wrong.Layout, wrong.Parts, wrong.PayloadSHA256)
	r.ActivePackage.PartsComposition = wrong
	_, disposition = runtime.observeLostCoreLoad(context.Background(), compositionStatusControl{r}, staged, Protocol2Response{State: "idle"})
	if disposition == coreLoadConfirmed || matchingAdoptedPackage([]corepackage.Staged{staged}, *r.ActivePackage) != -1 {
		t.Fatal("raster tuple falsely confirmed native selection")
	}
}

func TestNativePartsStatusRejectsBareAndCPUOnlyActivePackage(t *testing.T) {
	for _, cpu := range []bool{false, true} {
		r, _ := partsResponseLayout(t, expansion.ColecoNativeVideoLayout)
		r.ActivePackage.PartsComposition = nil
		if cpu {
			_, c := compositionResponse(t)
			r.ActivePackage.Composition = &c
		}
		if validProtocol2Response(r) {
			t.Fatalf("native shell without video parts accepted: cpu=%t", cpu)
		}
		// Raster shells still provide built-in output with an optional CPU part.
		r.ActivePackage.Descriptor.Interfaces[3].ID = expansion.VideoSlot
		if !validProtocol2Response(r) {
			t.Fatalf("existing raster base/CPU status rejected: cpu=%t", cpu)
		}
	}
}

func TestPartsStatusRequiresDeclaredShellAndExactUnchangedCapabilities(t *testing.T) {
	r, c := partsResponse(t)
	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeProtocol2Response(encoded); err != nil {
		t.Fatalf("valid parts response: %v", err)
	}
	for name, change := range map[string]func(*Protocol2Response){
		"unmarked shell": func(r *Protocol2Response) {
			r.ActivePackage.Descriptor.Interfaces = r.ActivePackage.Descriptor.Interfaces[:3]
		},
		"other core":      func(r *Protocol2Response) { r.ActivePackage.Descriptor.Core.ID = "fes.other" },
		"required marker": func(r *Protocol2Response) { r.ActivePackage.Descriptor.Interfaces[3].Required = true },
		"persistent":      func(r *Protocol2Response) { r.ActivePackage.PersistenceMode = "persistent" },
		"wrong digest":    func(r *Protocol2Response) { r.ActivePackage.PartsComposition.PayloadSHA256 = strings.Repeat("e", 64) },
		"unregistered operational capability": func(r *Protocol2Response) {
			r.ActivePackage.Descriptor.Interfaces = append(r.ActivePackage.Descriptor.Interfaces, corepackage.Interface{ID: "unknown.operational", Major: 1, Required: true})
		},
	} {
		t.Run(name, func(t *testing.T) {
			var bad Protocol2Response
			if err := json.Unmarshal(encoded, &bad); err != nil {
				t.Fatal(err)
			}
			change(&bad)
			if validProtocol2Response(bad) {
				t.Fatal("accepted invalid parts status")
			}
		})
	}
	activation := activationFromProtocol2(c.PackageID, r.ActivePackage.Descriptor, r)
	status := corePackageStatus(activation)
	if !reflect.DeepEqual(status.PartsComposition, &c) || status.BuildID != r.ActivePackage.Descriptor.Build.ID || len(status.ActiveInterfaces) != 2 {
		t.Fatal("parts altered base identity or capabilities")
	}
	r.ActivePackage.PartsComposition.Parts[0].PartID = strings.Repeat("e", 64)
	if activation.PartsComposition.Parts[0].PartID != c.Parts[0].PartID {
		t.Fatal("activation aliases role selection")
	}
	staged := corepackage.Staged{PackageID: c.PackageID, Descriptor: r.ActivePackage.Descriptor, PartsComposition: &c}
	if matchingAdoptedPackage([]corepackage.Staged{staged}, *r.ActivePackage) != -1 {
		t.Fatal("adopted different presentation parts")
	}
}

func TestPartsClientRequiresExactReturnedComposition(t *testing.T) {
	r, c := partsResponse(t)
	for _, wrong := range []bool{false, true} {
		current := r
		a := *r.ActivePackage
		current.ActivePackage = &a
		if wrong {
			other := c
			other.PayloadSHA256 = strings.Repeat("e", 64)
			other.ID, _ = expansion.PartsCompositionID(other.PackageID, other.Layout, other.Parts, other.PayloadSHA256)
			a.PartsComposition = &other
		}
		encoded, _ := json.Marshal(current)
		socket := newSequenceSocketFixture(t, []string{fixtureLines(t, "protocol-v2.jsonl")[1] + "\n", string(encoded) + "\n"})
		_, err := NewClient(socket.path).LoadPartsCore(context.Background(), "/base", c.PackageID, "/linked/linked.rbf", []PartPath{{"video", "/video"}}, c)
		if (err != nil) != wrong {
			t.Fatalf("wrong=%v: %v", wrong, err)
		}
	}
}

func TestLostPartsResponseRequiresExactSelectionForConfirmation(t *testing.T) {
	response, composition := partsResponse(t)
	staged := corepackage.Staged{PackageID: composition.PackageID,
		Descriptor: response.ActivePackage.Descriptor, PartsComposition: clonePartsComposition(&composition)}
	before := Protocol2Response{State: "idle"}
	runtime := &Runtime{}
	_, disposition := runtime.observeLostCoreLoad(context.Background(), compositionStatusControl{response}, staged, before)
	if disposition != coreLoadConfirmed {
		t.Fatalf("matching selection disposition=%v", disposition)
	}
	other := clonePartsComposition(&composition)
	other.Parts[0].PartID = strings.Repeat("e", 64)
	other.ID, _ = expansion.PartsCompositionID(other.PackageID, other.Layout, other.Parts, other.PayloadSHA256)
	response.ActivePackage.PartsComposition = other
	_, disposition = runtime.observeLostCoreLoad(context.Background(), compositionStatusControl{response}, staged, before)
	if disposition != coreLoadRecovery {
		t.Fatal("different video selection falsely confirmed", disposition)
	}
	response.ActivePackage.PartsComposition = nil
	_, disposition = runtime.observeLostCoreLoad(context.Background(), compositionStatusControl{response}, staged, before)
	if disposition != coreLoadRecovery {
		t.Fatal("plain shell falsely confirmed", disposition)
	}
}

func TestNativePartsFixtureMatchesTypedIdentity(t *testing.T) {
	for _, line := range fixtureLines(t, "protocol-v2-parts-responses.jsonl") {
		response, err := decodeProtocol2Response([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		c := response.ActivePackage.PartsComposition
		if c == nil || !validPartsComposition(*c, response.ActivePackage.PackageID) || response.ActivePackage.Composition != nil || len(response.Capabilities.ActiveInterfaces) != 1 {
			t.Fatal("native serializer and Go typed parts disagree")
		}
	}
}

func TestLibraryPartsClientRequiresDataRootAndExactReturnedTuple(t *testing.T) {
	r, c := partsResponse(t)
	for _, wrong := range []bool{false, true} {
		current := r
		a := *r.ActivePackage
		current.ActivePackage = &a
		if wrong {
			other := c
			other.PayloadSHA256 = strings.Repeat("e", 64)
			other.ID, _ = expansion.PartsCompositionID(other.PackageID, other.Layout, other.Parts, other.PayloadSHA256)
			a.PartsComposition = &other
		}
		encoded, _ := json.Marshal(current)
		socket := newSequenceSocketFixture(t, []string{fixtureLines(t, "protocol-v2.jsonl")[1] + "\n", string(encoded) + "\n"})
		client := NewClient(socket.path)
		if _, err := client.LoadLibraryPartsCore(context.Background(), "/base", c.PackageID, "relative", "/linked/linked.rbf", []PartPath{{"video", "/video"}}, c); err == nil {
			t.Fatal("accepted invalid data root")
		}
		_, err := client.LoadLibraryPartsCore(context.Background(), "/base", c.PackageID, CoreDataRoot, "/linked/linked.rbf", []PartPath{{"video", "/video"}}, c)
		if (err != nil) != wrong {
			t.Fatalf("wrong=%v error=%v", wrong, err)
		}
		requests := socket.wait(t)
		var request map[string]any
		if len(requests) != 2 || json.Unmarshal([]byte(requests[1]), &request) != nil || request["operation"] != "load_parts_library_core" || request["data_root"] != CoreDataRoot || request["package_id"] != c.PackageID {
			t.Fatalf("requests=%v", requests)
		}
	}
}
