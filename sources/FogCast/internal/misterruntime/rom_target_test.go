package misterruntime_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/misteross/expansion"
)

// romTargetInput uses the Mistral-generated CRAM map and blank bitstream. No
// programmed output is supplied by the host; LoadCoreOwned must derive it.
func romTargetInput(t *testing.T) corepackage.ROMInput {
	t.Helper()
	readGzip := func(name string) []byte {
		f, err := os.Open(filepath.Join("..", "..", "..", "misteross", "expansion", "testdata", "rom", name+".gz"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		r, err := gzip.NewReader(f)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		data, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	base, mapping, rom := readGzip("blank.rbf"), readGzip("map.json"), readGzip("ramp.rom")
	fixture := filepath.Join("..", "..", "corepackage", "testdata", "core-bundle-v2")
	manifest, err := os.ReadFile(filepath.Join(fixture, "manifests", "valid-basic.toml"))
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(filepath.Join(fixture, "payloads", "fes-fixture.rbf"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(manifest), "format = 2", "format = 3", 1)
	text = strings.ReplaceAll(text, fmt.Sprintf("%x", sha256.Sum256(old)), fmt.Sprintf("%x", sha256.Sum256(base)))
	text = strings.ReplaceAll(text, fmt.Sprintf("size = %d", len(old)), fmt.Sprintf("size = %d", len(base)))
	text += fmt.Sprintf("\n[rom]\nid = \"machine\"\nrole = \"firmware\"\nsource_size = %d\nfile = \"rom-map.json\"\nsize = %d\nsha256 = \"%x\"\n", len(rom), len(mapping), sha256.Sum256(mapping))
	archive := tarCoreArchive(t, []byte(text), base)
	archive = archive[:len(archive)-1024]
	// Reuse the shared restricted-ustar writer, changing its first member's
	// name and checksum to append the sealed map in canonical order.
	mapArchive := tarCoreArchive(t, mapping, nil)
	header := append([]byte(nil), mapArchive[:512]...)
	clear(header[:100])
	copy(header, "rom-map.json")
	for i := 148; i < 156; i++ {
		header[i] = ' '
	}
	sum := 0
	for _, value := range header {
		sum += int(value)
	}
	copy(header[148:156], fmt.Sprintf("%06o\x00 ", sum))
	archive = append(archive, header...)
	archive = append(archive, mapping...)
	archive = append(archive, make([]byte, (512-len(mapping)%512)%512+1024)...)
	return corepackage.ROMInput{Package: archive, ROM: rom}
}

type romTargetControl struct {
	packageControl
	t             *testing.T
	linkedCalls   int
	linkedPath    string
	link          corepackage.ROMLinkIdentity
	mismatchReply bool
	dropReply     bool
}

func (c *romTargetControl) LoadROMLinkedCore(ctx context.Context, path, id, root, expansionPath, payloadPath string, composition *expansion.Composition, programmedPath string, link corepackage.ROMLinkIdentity) (misterruntime.Protocol2Response, error) {
	c.linkedCalls++
	if root != "" || expansionPath != "" || composition != nil {
		c.t.Fatalf("unexpected library/composition context: %q %q %#v", root, expansionPath, composition)
	}
	programmed, err := os.ReadFile(programmedPath)
	if err != nil {
		return misterruntime.Protocol2Response{}, err
	}
	base, err := os.ReadFile(filepath.Join(path, "core.rbf"))
	if err != nil {
		return misterruntime.Protocol2Response{}, err
	}
	if bytes.Equal(programmed, base) || link.ProgrammedSHA256 != fmt.Sprintf("%x", sha256.Sum256(programmed)) || link.ProgrammedSize != int64(len(programmed)) {
		c.t.Fatal("target did not derive and bind the programmed ROM bitstream")
	}
	c.linkedPath, c.link = programmedPath, link
	response, err := c.packageControl.LoadCore(ctx, path, id)
	if err == nil && response.ActivePackage != nil {
		response.Capabilities.ROMLinking = 1
		if c.mismatchReply {
			link.SourceSHA256 = strings.Repeat("0", 64)
		}
		response.ActivePackage.ROMLink = &link
		c.status2 = &response
	}
	if c.dropReply && err == nil {
		return misterruntime.Protocol2Response{}, io.EOF
	}
	return response, err
}

func newROMTargetControl(t *testing.T, capability uint64) *romTargetControl {
	return &romTargetControl{t: t, packageControl: packageControl{status2: &misterruntime.Protocol2Response{
		Protocol: 2, OK: true, State: "idle", Execution: "none", Version: "test",
		Capabilities: misterruntime.Protocol2Capabilities{ROMLinking: capability},
	}}}
}

func TestROMTargetLinksSourceEnvelopeBeforeRuntimeDispatch(t *testing.T) {
	input := romTargetInput(t)
	envelope, err := corepackage.WriteROMInput(input)
	if err != nil {
		t.Fatal(err)
	}
	control := newROMTargetControl(t, 1)
	barrier := &replacementBarrier{}
	root := t.TempDir()
	runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(root), misterruntime.WithCoreReplacementBarrier(barrier))
	active, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(envelope)), bytes.NewReader(envelope))
	if apiErr != nil || !attempted || control.linkedCalls != 1 || barrier.begins != 1 || active.ROMLink == nil || *active.ROMLink != control.link {
		t.Fatalf("activation=%#v attempted=%v error=%v calls=%d barriers=%d", active, attempted, apiErr, control.linkedCalls, barrier.begins)
	}
	if control.link.SourceSHA256 != fmt.Sprintf("%x", sha256.Sum256(input.ROM)) || control.link.SourceSize != int64(len(input.ROM)) {
		t.Fatal("source identity lost")
	}
	if _, err := os.Stat(control.linkedPath); err != nil {
		t.Fatalf("active programmed artifact not retained: %v", err)
	}
	if _, apiErr := runtime.Stop(context.Background()); apiErr != nil {
		t.Fatal(apiErr)
	}
	if _, err := os.Stat(control.linkedPath); !os.IsNotExist(err) {
		t.Fatalf("stopped programmed artifact remains: %v", err)
	}
}

func TestROMTargetRejectsBeforeReplacement(t *testing.T) {
	input := romTargetInput(t)
	envelope, err := corepackage.WriteROMInput(input)
	if err != nil {
		t.Fatal(err)
	}
	corrupted := bytes.Replace(envelope, []byte(fmt.Sprintf("%x", sha256.Sum256(input.ROM))), []byte(strings.Repeat("0", 64)), 1)
	if bytes.Equal(corrupted, envelope) {
		t.Fatal("source digest fixture not found")
	}
	for _, test := range []struct {
		name       string
		capability uint64
		body       []byte
		cancel     bool
	}{
		{"capability absent", 0, envelope, false}, {"capability unknown", 2, envelope, false},
		{"source digest mismatch", 1, corrupted, false}, {"bare format 3", 1, input.Package, false},
		{"cancelled admission", 1, envelope, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			control := newROMTargetControl(t, test.capability)
			barrier := &replacementBarrier{}
			root := t.TempDir()
			runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(root), misterruntime.WithCoreReplacementBarrier(barrier))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.cancel {
				cancel()
			}
			_, attempted, apiErr := runtime.LoadCoreOwned(ctx, context.Background(), context.Background(), int64(len(test.body)), bytes.NewReader(test.body))
			if apiErr == nil || attempted || control.loadCalls != 0 || control.linkedCalls != 0 || barrier.begins != 0 {
				t.Fatalf("attempted=%v error=%v loads=%d ROM loads=%d barriers=%d", attempted, apiErr, control.loadCalls, control.linkedCalls, barrier.begins)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatalf("failed admission left artifacts: %v %v", entries, err)
			}
		})
	}
}

func TestROMTargetCancellationAtFinalStatusPrecedesReplacement(t *testing.T) {
	input := romTargetInput(t)
	envelope, err := corepackage.WriteROMInput(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"admission", "observation", "owner"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			admission, observation, owner := context.Background(), context.Background(), context.Background()
			switch name {
			case "admission":
				admission = ctx
			case "observation":
				observation = ctx
			case "owner":
				owner = ctx
			}
			control := newROMTargetControl(t, 1)
			control.beforeStatus = func(call int) {
				if call == 2 {
					cancel()
				}
			}
			barrier := &replacementBarrier{}
			root := t.TempDir()
			runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(root), misterruntime.WithCoreReplacementBarrier(barrier))
			_, attempted, apiErr := runtime.LoadCoreOwned(admission, observation, owner, int64(len(envelope)), bytes.NewReader(envelope))
			if apiErr == nil || attempted || control.linkedCalls != 0 || control.loadCalls != 0 || barrier.begins != 0 {
				t.Fatalf("attempted=%v error=%v ROM calls=%d calls=%d barriers=%d", attempted, apiErr, control.linkedCalls, control.loadCalls, barrier.begins)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatalf("cancelled admission left artifacts: %v %v", entries, err)
			}
		})
	}
}

func TestROMTargetLostReplyReconciliationRequiresMatchingROM(t *testing.T) {
	input := romTargetInput(t)
	envelope, err := corepackage.WriteROMInput(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, mismatch := range []bool{false, true} {
		t.Run(fmt.Sprintf("mismatched=%v", mismatch), func(t *testing.T) {
			control := newROMTargetControl(t, 1)
			control.dropReply = true
			control.mismatchReply = mismatch
			runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 20*time.Millisecond, misterruntime.WithCorePackageRoot(t.TempDir()))
			defer func() {
				if _, apiErr := runtime.Stop(context.Background()); apiErr != nil {
					t.Errorf("stop after lost reply: %v", apiErr)
				}
			}()
			active, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(envelope)), bytes.NewReader(envelope))
			if !attempted || control.linkedCalls != 1 || control.status2Calls < 3 {
				t.Fatalf("attempted=%v loads=%d status reads=%d", attempted, control.linkedCalls, control.status2Calls)
			}
			if mismatch {
				if apiErr == nil || active.ROMLink != nil {
					t.Fatalf("wrong observed ROM accepted: activation=%#v error=%v", active, apiErr)
				}
			} else if apiErr != nil || active.ROMLink == nil || *active.ROMLink != control.link {
				t.Fatalf("requested ROM not reconciled: activation=%#v error=%v", active, apiErr)
			}
		})
	}
}

func TestROMTargetRejectsMismatchedRuntimeReceipt(t *testing.T) {
	input := romTargetInput(t)
	envelope, err := corepackage.WriteROMInput(input)
	if err != nil {
		t.Fatal(err)
	}
	control := newROMTargetControl(t, 1)
	control.mismatchReply = true
	runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(t.TempDir()))
	defer func() {
		if _, apiErr := runtime.Stop(context.Background()); apiErr != nil {
			t.Errorf("stop after ambiguous receipt: %v", apiErr)
		}
	}()
	active, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(envelope)), bytes.NewReader(envelope))
	if apiErr == nil || !attempted || active.ROMLink != nil || control.linkedCalls != 1 {
		t.Fatalf("activation=%#v attempted=%v error=%v calls=%d", active, attempted, apiErr, control.linkedCalls)
	}
}
