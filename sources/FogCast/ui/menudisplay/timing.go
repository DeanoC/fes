package menudisplay

// Timing splits one Present for frame-time diagnostics. Begin, Stage and
// Commit are measured by this client; the Runtime fields come from the
// runtime's menu_frame.timing reply.
type Timing struct {
	// BeginUS covers mapping a new generation's grant, dialling the runtime
	// socket and the menu_frame_begin round trip.
	BeginUS int64
	// StageUS is writing the frame's pixels into the granted slot.
	StageUS int64
	// CommitUS is the menu_frame_commit round trip, which includes the runtime work.
	CommitUS int64
	// Runtime-side parts of the commit: writing back the client's lines,
	// the slot submit and the wait until scanout shows it.
	RuntimeCleanUS, RuntimeSubmitUS, RuntimeScanoutWaitUS int64
	// StagedPixels is how many pixels this client wrote into the slot: the
	// whole frame, or this frame's and the previous frame's damage.
	StagedPixels int64
}
