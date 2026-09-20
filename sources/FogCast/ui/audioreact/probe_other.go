//go:build !linux

package audioreact

// Probe reports no measured source on non-Linux hosts. Kit chrome runs on
// the CGO-free linuxfb adapter; sofa Mac builds do not gain a fake meter.
func Probe() Report {
	return Report{
		Measured: false,
		Notes:    "audio probe is Linux-only; no measured level on this OS.",
	}
}
