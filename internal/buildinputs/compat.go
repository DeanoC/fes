package buildinputs

// expectedRuntimeCommit is the libmister-runtime pin this FogCast tree was
// selected against. Tests require it to match build/native-runtime.inputs.lock.toml.
const expectedRuntimeCommit = "8c4b690964ca2af06581e4cf11df22d33f48e1e0"

// ExpectedRuntimeCommit is the selected build provenance, not a connection gate.
func ExpectedRuntimeCommit() string {
	return expectedRuntimeCommit
}
