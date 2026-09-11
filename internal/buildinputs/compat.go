package buildinputs

import "github.com/DeanoC/FogCast/protocol"

// expectedRuntimeCommit is the libmister-runtime pin this FogCast tree was
// selected against. Tests require it to match build/native-runtime.inputs.lock.toml.
const expectedRuntimeCommit = "a729acc593ec772fa5ecd5f802e2dee9758bd4dc"

// ExpectedRuntimeCommit is the runtime git identity this host admits.
func ExpectedRuntimeCommit() string {
	return expectedRuntimeCommit
}

// Check reports the first sealed-artifact field that disagrees with this host.
// Empty result means there is no comparable pair (legacy image, unknown
// revisions, or a match). It never rewrites configuration.
func Check(hostRevision, expectedRuntime string, artifacts *protocol.Artifacts) (field, expected, observed string) {
	if artifacts == nil {
		return "", "", ""
	}
	if commitValue.MatchString(expectedRuntime) && commitValue.MatchString(artifacts.RuntimeCommit) &&
		artifacts.RuntimeCommit != expectedRuntime {
		return "runtime_commit", expectedRuntime, artifacts.RuntimeCommit
	}
	if commitValue.MatchString(hostRevision) && commitValue.MatchString(artifacts.AgentRevision) &&
		artifacts.AgentRevision != hostRevision {
		return "agent_revision", hostRevision, artifacts.AgentRevision
	}
	return "", "", ""
}
