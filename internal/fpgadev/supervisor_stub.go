//go:build !fpgadev

package fpgadev

// Dependencies remains an inert type on unsupported builds so command stubs
// can retain a stable function signature without linking supervisor logic.
type Dependencies struct{}

// ProcessAttestation keeps the untagged child stub's method set source-
// compatible without exposing any Linux process implementation.
type ProcessAttestation struct {
	PID, StartTime, Device, Inode uint64
	SHA256                        string
}
