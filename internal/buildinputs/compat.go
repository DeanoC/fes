package buildinputs

// expectedRuntimeCommit is the libmister-runtime pin this FogCast tree was
// selected against. Tests require it to match build/native-runtime.inputs.lock.toml.
const expectedRuntimeCommit = "a6d658cd305c4a84860afc1f8b00a2798ee6e4f4"

// ExpectedRuntimeCommit is the selected build provenance, not a connection gate.
func ExpectedRuntimeCommit() string {
	return expectedRuntimeCommit
}
