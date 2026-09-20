package protocol

// CheckAPIVersion admits the explicit target-agent contract, not build provenance.
func CheckAPIVersion(version string) error {
	if version != "v1" {
		return &APIError{Code: CodeVersionMismatch, Message: "target API version is missing or unsupported; expected v1"}
	}
	return nil
}
