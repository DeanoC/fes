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

func testAuthority() policy.Authority {
	return policy.Authority{UpstreamCommit: strings.Repeat("1", 40), UpstreamTree: strings.Repeat("2", 40), ForkCommit: strings.Repeat("3", 40), ForkTree: strings.Repeat("4", 40), ForkParentCommit: strings.Repeat("1", 40), PatchCommits: []string{strings.Repeat("3", 40)}, SourceDateEpoch: 0, VDate: "700101"}
}
