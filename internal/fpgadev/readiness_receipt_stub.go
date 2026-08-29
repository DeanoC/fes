//go:build !linux && !fpgadev

package fpgadev

// ReadinessReceipt keeps the unsupported, non-Linux runtime stub linkable
// without exposing the development supervisor protocol from an untagged
// Linux build.
type ReadinessReceipt struct {
	Schema           uint64
	PID              uint64
	StartTime        uint64
	ExecutableDevice uint64
	ExecutableInode  uint64
	ExecutableSHA256 string
	ProfileSHA256    string
	Capabilities     []string
}
