package main

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// These small payloads are deliberately synthetic parser/assembly controls.
// They are not Quartus outputs, partial bitstreams or hardware evidence.
func fixture(t *testing.T) (string, artifacts) {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"base.rbf":      {0x00, 0xff, 0x31, 0x80, 0x61, 0x02, 0x19},
		"persona_a.rbf": {0xff, 0x00, 0xa1, 0x55, 0x03},
		"persona_b.rbf": {0x00, 0xff, 0xb2, 0x77, 0x04, 0x05},
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	identity := func(name string) string { return digest([]byte("synthetic-" + name)) }
	a := artifacts{Format: 2, Kind: "fes.de25-pr-proof-artifacts", DiagnosticOnly: true, Device: "A5EB013BB23BE4SCS",
		Tool: toolIdentity{Version: "26.3.0 Build114", Edition: "SC Pro Edition", QuartusShSHA256: identity("sh"), QuartusAsmSHA256: identity("asm"), QuartusPfgSHA256: identity("pfg")},
		Shell: shellIdentity{ControlBuildID: "112233445566778899aabbccddeeff00", SourceRevision: strings.Repeat("1", 40), SourceClosureSHA256: identity("source"), RootQDBSHA256: identity("root"),
			InterfaceSHA256: identity("interface"), ClockResetSHA256: identity("clock-reset"), Base: blob{Path: "base.rbf", Size: int64(len(files["base.rbf"])), SHA256: digest(files["base.rbf"])},
			VendorPOF: vendorPOF{RegionID: "0x01000000", Hash: identity("pof"), InfoSHA256: identity("base-info")}},
		Region:     regionIdentity{ID: "slot0", Instance: "slot_inst", Placement: "X10 Y20 X12 Y24", Routing: "X9 Y19 X13 Y25"},
		Acceptance: acceptance{Physical: "not-run", Timing: "passed", BasePersonaCompatibility: "pfg-info-pof0-match"}}
	for _, id := range []string{"a", "b"} {
		name := "persona_" + id + ".rbf"
		a.Personas = append(a.Personas, persona{ID: "counter_" + id, HardwareID: uint32(len(a.Personas) + 1), Entity: "fes_link_persona_" + id, Revision: "persona_" + id,
			Path: name, Size: int64(len(files[name])), SHA256: digest(files[name]), RootQDBSHA256: a.Shell.RootQDBSHA256,
			InterfaceSHA256: a.Shell.InterfaceSHA256, ClockResetSHA256: a.Shell.ClockResetSHA256, RegionID: "slot0",
			VendorPOF: vendorPOF{RegionID: "0x01000000", Hash: a.Shell.VendorPOF.Hash, InfoSHA256: identity(id + "-info")}})
	}
	refreshShell(t, &a)
	writeFixture(t, directory, a)
	return directory, a
}

func refreshShell(t *testing.T, a *artifacts) {
	t.Helper()
	id, err := shellID(*a)
	if err != nil {
		t.Fatal(err)
	}
	a.Shell.ID = id
}

func writeFixture(t *testing.T, directory string, a artifacts) []byte {
	t.Helper()
	data, err := canonical(a)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(directory, "artifacts.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return data
}

func arguments(directory string, a artifacts, selection, output string) []string {
	return []string{"compose", "--artifacts", filepath.Join(directory, "artifacts.json"), "--expected-shell-id", a.Shell.ID,
		"--expected-base-sha256", a.Shell.Base.SHA256, "--persona", selection, "--output", output,
		"--expected-descriptor-sha256", fixtureDescriptorDigest(a)}
}

func fixtureDescriptorDigest(a artifacts) string {
	data, err := canonical(a)
	if err != nil {
		panic(err)
	}
	return digest(append(data, '\n'))
}

func invoke(t *testing.T, args []string, success bool) []byte {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := run(args, &stdout, &stderr)
	if success && err != nil {
		t.Fatalf("compose: %v (%s)", err, stderr.String())
	}
	if !success && (err == nil || stdout.Len() != 0) {
		t.Fatalf("invalid input returned err=%v, success output=%q", err, stdout.String())
	}
	return stdout.Bytes()
}

func readBundle(t *testing.T, filename string) (map[string][]byte, []byte) {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(bytes.NewReader(data))
	members := map[string][]byte{}
	for _, expected := range []string{"deployment.json", "artifacts.json", "base.rbf", "slot0/persona.rbf"} {
		header, err := reader.Next()
		if err != nil {
			t.Fatal(err)
		}
		if header.Name != expected || header.Format != tar.FormatUSTAR || header.Typeflag != tar.TypeReg || header.Mode != 0644 || header.Uid != 0 || header.Gid != 0 || header.ModTime.Unix() != 0 || header.PAXRecords != nil || header.Linkname != "" {
			t.Fatalf("unexpected deployment member: %+v", header)
		}
		members[header.Name], err = io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := reader.Next(); err != io.EOF {
		t.Fatalf("unexpected trailing archive member: %v", err)
	}
	return members, data
}

func TestComposeCopiesExactBytesAndOrdersDeployment(t *testing.T) {
	directory, a := fixture(t)
	output := filepath.Join(directory, "assembled.tar")
	stdout := invoke(t, arguments(directory, a, "counter_a", output), true)
	members, first := readBundle(t, output)
	for member, source := range map[string]string{"base.rbf": "base.rbf", "slot0/persona.rbf": "persona_a.rbf", "artifacts.json": "artifacts.json"} {
		actual, err := os.ReadFile(filepath.Join(directory, source))
		if err != nil || !bytes.Equal(members[member], actual) {
			t.Fatalf("%s did not preserve exact producer bytes", member)
		}
	}
	if !bytes.Equal(stdout, members["deployment.json"]) {
		t.Fatal("stdout differs from bundled plan")
	}
	var plan deployment
	if err := json.Unmarshal(stdout, &plan); err != nil {
		t.Fatal(err)
	}
	if !plan.DiagnosticOnly || plan.MonolithicRBF || plan.PhysicalAcceptance != "not-run" || plan.Controller != "runtime-protocol-only" ||
		plan.ShellID != a.Shell.ID || len(plan.Steps) != 2 || plan.Steps[0].Action != "load-base" || plan.Steps[1].Action != "load-persona" ||
		plan.Steps[1].Persona != "counter_a" || plan.Steps[1].RegionID != "slot0" || plan.Steps[1].SHA256 != a.Personas[0].SHA256 {
		t.Fatalf("incorrect plan or qualification boundary: %+v", plan)
	}
	// Reassembly is byte deterministic and independent of output path/mtime.
	other := filepath.Join(directory, "same.tar")
	invoke(t, arguments(directory, a, "counter_a", other), true)
	_, second := readBundle(t, other)
	if !bytes.Equal(first, second) {
		t.Fatal("same inputs produce different deployment bytes")
	}
	other = filepath.Join(directory, "alternative.tar")
	invoke(t, arguments(directory, a, "counter_b", other), true)
	alternative, _ := readBundle(t, other)
	if bytes.Equal(members["slot0/persona.rbf"], alternative["slot0/persona.rbf"]) || bytes.Equal(stdout, alternative["deployment.json"]) {
		t.Fatal("selecting a different actual persona did not change the assembled bytes and identity")
	}
}

func TestRefusesInvalidContractEvenWithFreshShellID(t *testing.T) {
	cases := map[string]func(*artifacts){
		"missing control identity":            func(a *artifacts) { a.Shell.ControlBuildID = "" },
		"zero control identity":               func(a *artifacts) { a.Shell.ControlBuildID = strings.Repeat("0", 32) },
		"missing persona hardware identity":   func(a *artifacts) { a.Personas[0].HardwareID = 0 },
		"duplicate persona hardware identity": func(a *artifacts) { a.Personas[1].HardwareID = a.Personas[0].HardwareID },
		"unsupported format":                  func(a *artifacts) { a.Format = 1 },
		"wrong device":                        func(a *artifacts) { a.Device = "5CSEBA6U23I7" },
		"wrong tool version":                  func(a *artifacts) { a.Tool.Version = "26.2.0 Build1" },
		"wrong edition":                       func(a *artifacts) { a.Tool.Edition = "Lite Edition" },
		"missing tool identity":               func(a *artifacts) { a.Tool.QuartusPfgSHA256 = "" },
		"physical qualification claim":        func(a *artifacts) { a.Acceptance.Physical = "passed" },
		"failed timing":                       func(a *artifacts) { a.Acceptance.Timing = "failed" },
		"compatibility absent":                func(a *artifacts) { a.Acceptance.BasePersonaCompatibility = "unchecked" },
		"unknown region":                      func(a *artifacts) { a.Region.ID = "slot1" },
		"unbounded region":                    func(a *artifacts) { a.Region.Routing = "X0 Y0 X999999 Y999999" },
		"reversed region":                     func(a *artifacts) { a.Region.Placement = "X12 Y24 X10 Y20" },
		"foreign root QDB":                    func(a *artifacts) { a.Personas[0].RootQDBSHA256 = digest([]byte("other shell")) },
		"foreign interface":                   func(a *artifacts) { a.Personas[0].InterfaceSHA256 = digest([]byte("other wires")) },
		"foreign clock reset":                 func(a *artifacts) { a.Personas[0].ClockResetSHA256 = digest([]byte("other clock")) },
		"foreign region":                      func(a *artifacts) { a.Personas[0].RegionID = "slot1" },
		"foreign vendor POF hash":             func(a *artifacts) { a.Personas[0].VendorPOF.Hash = digest([]byte("foreign POF")) },
		"foreign vendor POF region":           func(a *artifacts) { a.Personas[0].VendorPOF.RegionID = "0x02000000" },
		"missing vendor info identity":        func(a *artifacts) { a.Personas[0].VendorPOF.InfoSHA256 = "" },
		"zero vendor POF":                     func(a *artifacts) { a.Shell.VendorPOF.Hash = strings.Repeat("0", 64) },
		"duplicate persona":                   func(a *artifacts) { a.Personas[1].ID = a.Personas[0].ID },
		"duplicate artifact path":             func(a *artifacts) { a.Personas[1].Path = a.Personas[0].Path },
		"duplicate compiler revision":         func(a *artifacts) { a.Personas[1].Revision = a.Personas[0].Revision },
		"unsorted alternatives":               func(a *artifacts) { a.Personas[0], a.Personas[1] = a.Personas[1], a.Personas[0] },
		"escape artifact":                     func(a *artifacts) { a.Personas[0].Path = "../outside.rbf" },
		"absolute artifact":                   func(a *artifacts) { a.Personas[0].Path = "/dev/mem" },
		"unbounded artifact":                  func(a *artifacts) { a.Personas[0].Size = maxArtifactBytes + 1 },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			directory, a := fixture(t)
			change(&a)
			refreshShell(t, &a)
			writeFixture(t, directory, a)
			output := filepath.Join(directory, "refused.tar")
			invoke(t, arguments(directory, a, "counter_a", output), false)
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatal("rejection left a deployment artifact")
			}
		})
	}
}

func TestExternalSelectionAndOverlapAreRequired(t *testing.T) {
	directory, a := fixture(t)
	for _, change := range []func([]string) []string{
		func(args []string) []string { args[4] = strings.Repeat("0", 64); return args },
		func(args []string) []string { args[6] = strings.Repeat("0", 64); return args },
		func(args []string) []string { args[4] = ""; return args },
		func(args []string) []string { args[12] = ""; return args },
		func(args []string) []string { args[8] = "missing"; return args },
		func(args []string) []string { return append(args, "--persona", "counter_b") },
		func(args []string) []string { return append(args, "--persona", "counter_a") },
	} {
		output := filepath.Join(directory, "refused.tar")
		invoke(t, change(arguments(directory, a, "counter_a", output)), false)
		if _, err := os.Lstat(output); !os.IsNotExist(err) {
			t.Fatal("bad external selection left output")
		}
	}
}

func TestPersonaSubstitutionCannotInheritTrustedCompilerDescriptor(t *testing.T) {
	directory, a := fixture(t)
	output := filepath.Join(directory, "refused.tar")
	args := arguments(directory, a, "counter_a", output)
	// A hostile or stale inventory can claim the same root/interface/reset while
	// replacing the selected bytes and their identity. The external descriptor
	// hash must remain the trusted original; shell/base pins alone are insufficient.
	replacement := []byte("different persona with unchanged claimed compatibility")
	if err := os.WriteFile(filepath.Join(directory, a.Personas[0].Path), replacement, 0600); err != nil {
		t.Fatal(err)
	}
	a.Personas[0].Size = int64(len(replacement))
	a.Personas[0].SHA256 = digest(replacement)
	a.Personas[0].Entity = "unreviewed_persona"
	a.Personas[0].Revision = "unreviewed_revision"
	writeFixture(t, directory, a)
	invoke(t, args, false)
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatal("substituted persona produced a bundle")
	}
}

func TestClosedCanonicalJSON(t *testing.T) {
	directory, a := fixture(t)
	valid := writeFixture(t, directory, a)
	for name, invalid := range map[string][]byte{
		"duplicate key":        append([]byte(`{"format":2,`), valid[1:]...),
		"unknown field":        append([]byte(`{"arbitrary":1,`), valid[1:]...),
		"missing field":        bytes.Replace(valid, []byte(`"diagnostic_only":true,`), nil, 1),
		"extra document":       append(append([]byte(nil), valid...), []byte("{}")...),
		"whitespace":           append([]byte(" "), valid...),
		"no canonical newline": valid[:len(valid)-1],
		"numeric coercion":     bytes.Replace(valid, []byte(`"format":2`), []byte(`"format":2.0`), 1),
		"truncation":           valid[:len(valid)/2],
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseArtifacts(invalid); err == nil {
				t.Fatal("invalid descriptor accepted")
			}
		})
	}
	if _, err := parseArtifacts(valid); err != nil {
		t.Fatal(err)
	}
}

func TestChangedTruncatedAndUnselectedArtifactFailWithoutOutput(t *testing.T) {
	for _, name := range []string{"base.rbf", "persona_a.rbf", "persona_b.rbf"} {
		for _, truncate := range []bool{false, true} {
			t.Run(name+"/"+map[bool]string{false: "changed", true: "truncated"}[truncate], func(t *testing.T) {
				directory, a := fixture(t)
				filename := filepath.Join(directory, name)
				data, err := os.ReadFile(filename)
				if err != nil {
					t.Fatal(err)
				}
				if truncate {
					data = data[:len(data)-1]
				} else {
					data[len(data)-1] ^= 1
				}
				if err := os.WriteFile(filename, data, 0600); err != nil {
					t.Fatal(err)
				}
				output := filepath.Join(directory, "refused.tar")
				invoke(t, arguments(directory, a, "counter_a", output), false)
				if _, err := os.Lstat(output); !os.IsNotExist(err) {
					t.Fatal("bad bytes left output")
				}
			})
		}
	}
}

func TestRefusesSymlinksAndFIFO(t *testing.T) {
	for _, form := range []string{"symlink", "fifo"} {
		t.Run(form, func(t *testing.T) {
			directory, a := fixture(t)
			filename := filepath.Join(directory, "persona_a.rbf")
			if err := os.Remove(filename); err != nil {
				t.Fatal(err)
			}
			var err error
			if form == "symlink" {
				err = os.Symlink("persona_b.rbf", filename)
			} else {
				err = syscall.Mkfifo(filename, 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			invoke(t, arguments(directory, a, "counter_a", filepath.Join(directory, "refused.tar")), false)
		})
	}
}

func TestBundleRechecksCopiedBytesAndNeverOverwrites(t *testing.T) {
	directory, a := fixture(t)
	receipt := writeFixture(t, directory, a)
	p := a.Personas[0]
	_, plan, err := makeDeployment(a, p, receipt)
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	// The source was valid at admission but changes before actual assembly.
	if err := copyBlob(root, blob{p.Path, p.Size, p.SHA256}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, p.Path), bytes.Repeat([]byte{1}, int(p.Size)), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(directory, "refused.tar")
	if err := writeBundle(root, a, p, receipt, plan, output); err == nil {
		t.Fatal("assembly copied changed bytes")
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatal("failed assembly exposed a partial bundle")
	}
	leftovers, _ := filepath.Glob(filepath.Join(directory, ".de25-pr-proof-*"))
	if len(leftovers) != 0 {
		t.Fatalf("partial temporary artifacts remain: %v", leftovers)
	}
	if err := os.WriteFile(output, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeBundle(root, a, p, receipt, plan, output); err == nil {
		t.Fatal("existing output overwritten")
	}
	data, _ := os.ReadFile(output)
	if string(data) != "existing" {
		t.Fatal("existing output changed")
	}
}

func TestOutputDirectoryMustBePrivate(t *testing.T) {
	directory, a := fixture(t)
	for _, mode := range []os.FileMode{0755, 0770, 0777} {
		if err := os.Chmod(directory, mode); err != nil {
			t.Fatal(err)
		}
		output := filepath.Join(directory, "refused.tar")
		invoke(t, arguments(directory, a, "counter_a", output), false)
		if _, err := os.Lstat(output); !os.IsNotExist(err) {
			t.Fatal("shared directory received an output")
		}
	}
}
