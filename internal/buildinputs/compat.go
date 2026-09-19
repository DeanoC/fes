package buildinputs

// expectedRuntimeCommit is the libmister-runtime pin this FogCast tree was
// selected against. Tests require it to match build/native-runtime.inputs.lock.toml.
const expectedRuntimeCommit = "079b4548d5ab84c094686e00df4fbbe658f4f613"

// ExpectedRuntimeCommit is the selected build provenance, not a connection gate.
func ExpectedRuntimeCommit() string {
	return expectedRuntimeCommit
}
