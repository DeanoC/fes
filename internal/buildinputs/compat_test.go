package buildinputs

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

func TestExpectedRuntimeCommitMatchesNativeLock(t *testing.T) {
	t.Parallel()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "build", "native-runtime.inputs.lock.toml"))
	if err != nil {
		t.Fatal(err)
	}
	want := "commit = '" + ExpectedRuntimeCommit() + "'"
	if !strings.Contains(string(data), want) {
		t.Fatalf("lock does not contain %s", want)
	}
}

func TestCheckIgnoresMissingOrUnknownArtifacts(t *testing.T) {
	t.Parallel()
	if field, _, _ := Check(strings.Repeat("a", 40), ExpectedRuntimeCommit(), nil); field != "" {
		t.Fatalf("nil artifacts: %s", field)
	}
	if field, _, _ := Check("unknown", ExpectedRuntimeCommit(), &protocol.Artifacts{RuntimeCommit: ExpectedRuntimeCommit()}); field != "" {
		t.Fatalf("matching runtime: %s", field)
	}
}

func TestCheckReportsRuntimeCommitMismatch(t *testing.T) {
	t.Parallel()
	observed := strings.Repeat("b", 40)
	field, expected, got := Check("unknown", ExpectedRuntimeCommit(), &protocol.Artifacts{RuntimeCommit: observed})
	if field != "runtime_commit" || expected != ExpectedRuntimeCommit() || got != observed {
		t.Fatalf("%s %s %s", field, expected, got)
	}
}

func TestCheckReportsAgentRevisionMismatch(t *testing.T) {
	t.Parallel()
	hostRev := strings.Repeat("c", 40)
	agentRev := strings.Repeat("d", 40)
	field, expected, got := Check(hostRev, ExpectedRuntimeCommit(), &protocol.Artifacts{
		RuntimeCommit: ExpectedRuntimeCommit(), AgentRevision: agentRev,
	})
	if field != "agent_revision" || expected != hostRev || got != agentRev {
		t.Fatalf("%s %s %s", field, expected, got)
	}
}
