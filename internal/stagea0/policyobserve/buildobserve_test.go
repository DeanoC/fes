package policyobserve

import (
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/stagea0/firstbuild"
	"github.com/DeanoC/FogCast-POC/internal/stagea0/policy"
)

func TestObserveCompileLinkNormalizesV1Commands(t *testing.T) {
	log := strings.Join([]string{
		`arm-none-linux-gnueabihf-gcc -I./ -DVALUE=\"1\" -MM main.cpp -MT bin/./main.cpp.d -MF bin/./main.cpp.d 2>&1 | sed -e 'x'`,
		`arm-none-linux-gnueabihf-gcc -I./lib -c -o bin/./main.cpp.o -c main.cpp 2>&1 | sed -e 'x'`,
		`arm-none-linux-gnueabihf-gcc -o bin/MiSTer bin/main.cpp.o -lstdc++`,
		`cp bin/MiSTer bin/MiSTer.elf`,
		`arm-none-linux-gnueabihf-strip bin/MiSTer`,
	}, "\n")
	document, err := ObserveCompileLink([]byte(log), testAuthority())
	if err != nil {
		t.Fatalf("ObserveCompileLink() = %v", err)
	}
	if document.CompileLink == nil || len(document.CompileLink.Records) != 5 {
		t.Fatalf("records = %#v, want five commands", document.CompileLink)
	}
	for _, record := range document.CompileLink.Records {
		if record.CWD != "/stage-a0/src" || strings.Contains(record.Argv[1], "./") || strings.Contains(record.Output, "/./") {
			t.Fatalf("record was not normalized: %#v", record)
		}
	}
	generated, err := ObserveGeneratedInput([]byte(log), nil, testAuthority())
	if err != nil || generated.GeneratedInput == nil || len(generated.GeneratedInput.Records) != 1 {
		t.Fatalf("ObserveGeneratedInput() = %#v, err=%v", generated, err)
	}
}

func TestObserveIntermediateMapsCapturedInventory(t *testing.T) {
	evidence := firstbuild.Evidence{Output: firstbuild.OutputEvidence{Inventory: []firstbuild.InventoryEntry{
		{Path: "bin/MiSTer", Mode: "0755"},
		{Path: "bin/MiSTer.elf", Mode: "0755"},
		{Path: "bin/main.cpp.d", Mode: "0644"},
		{Path: "bin/main.cpp.o", Mode: "0644"},
	}}}
	document, err := ObserveIntermediate(evidence, testAuthority())
	if err != nil {
		t.Fatalf("ObserveIntermediate() = %v", err)
	}
	if len(document.IntermediatePath.Records) != 4 || document.IntermediatePath.Records[0].Path != "bin/MiSTer" {
		t.Fatalf("records = %#v", document.IntermediatePath.Records)
	}
}

func TestObserveCompileLinkRejectsTraversalBeforeCleaning(t *testing.T) {
	_, err := ObserveCompileLink([]byte(`arm-none-linux-gnueabihf-gcc -c -o bin/obj/../main.o main.cpp`), testAuthority())
	if err == nil || !strings.Contains(err.Error(), "unsafe path") {
		t.Fatalf("traversal command error = %v", err)
	}
}

func TestObserveCompileLinkAllowsLogicalAbsolutePathWithoutTraversal(t *testing.T) {
	document, err := ObserveCompileLink([]byte(`arm-none-linux-gnueabihf-gcc -I/stage-a0/sysroot/usr/include -c -o bin/main.o main.cpp`), testAuthority())
	if err != nil {
		t.Fatalf("logical path command rejected: %v", err)
	}
	if got := document.CompileLink.Records[0].Argv[1]; got != "-I/stage-a0/sysroot/usr/include" {
		t.Fatalf("logical path changed to %q", got)
	}
	if _, err := ObserveCompileLink([]byte(`arm-none-linux-gnueabihf-gcc -I/stage-a0/src/../sysroot -c -o bin/main.o main.cpp`), testAuthority()); err == nil {
		t.Fatal("logical path traversal was accepted")
	}
}

func TestObserveCompileLinkDoesNotTreatOutputFlagValueAsLinkInput(t *testing.T) {
	document, err := ObserveCompileLink([]byte(`arm-none-linux-gnueabihf-gcc -o bin/output.o bin/input.o`), testAuthority())
	if err != nil {
		t.Fatalf("ObserveCompileLink() = %v", err)
	}
	record := document.CompileLink.Records[0]
	if len(record.OrderedInputs) != 1 || record.OrderedInputs[0] != "bin/input.o" {
		t.Fatalf("ordered inputs = %#v", record.OrderedInputs)
	}
}

func TestObserveCompileLinkPreservesBinaryLinkInput(t *testing.T) {
	document, err := ObserveCompileLink([]byte(`arm-none-linux-gnueabihf-ld -r -b binary -o bin/logo.png.o logo.png`), testAuthority())
	if err != nil {
		t.Fatalf("ObserveCompileLink() = %v", err)
	}
	record := document.CompileLink.Records[0]
	if len(record.OrderedInputs) != 1 || record.OrderedInputs[0] != "logo.png" {
		t.Fatalf("ordered inputs = %#v", record.OrderedInputs)
	}
}

func testAuthority() policy.Authority {
	return policy.Authority{UpstreamCommit: strings.Repeat("1", 40), UpstreamTree: strings.Repeat("2", 40), ForkCommit: strings.Repeat("3", 40), ForkTree: strings.Repeat("4", 40), ForkParentCommit: strings.Repeat("1", 40), PatchCommits: []string{strings.Repeat("3", 40)}, SourceDateEpoch: 0, VDate: "700101"}
}
