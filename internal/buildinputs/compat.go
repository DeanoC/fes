package buildinputs

// expectedRuntimeCommit is the libmister-runtime pin this FogCast tree was
// selected against. Tests require it to match build/native-runtime.inputs.lock.toml.
const expectedRuntimeCommit = "cbf884753bf3f5eee66de5604c057e0c5f77f77c"

// ExpectedRuntimeCommit is the selected build provenance, not a connection gate.
func ExpectedRuntimeCommit() string {
	return expectedRuntimeCommit
}
