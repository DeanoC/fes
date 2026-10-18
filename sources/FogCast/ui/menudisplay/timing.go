package menudisplay

// Timing splits one Present for frame-time diagnostics. Begin, Stage and
// Commit are measured by this client; the Runtime fields come from the
// runtime's menu_frame.timing reply and are zero when an older runtime
// does not report them.
type Timing struct {
	// BeginUS is dialling the runtime socket and the menu_frame_begin round trip.
	BeginUS int64
	// StageUS is mapping the staging memfd, copying the frame in and sealing it.
	StageUS int64
	// CommitUS is the menu_frame_commit round trip, which includes the runtime work.
	CommitUS int64
	// Runtime-side parts of the commit.
	RuntimeCopyUS, RuntimeSubmitUS, RuntimeScanoutWaitUS int64
	// StagedPixels is how many pixels this client copied into the staging
	// file: the whole frame, or the damaged rectangles.
	StagedPixels int64
	// RuntimeStoredPixels is how many pixels the runtime wrote to the DDR
	// slot, including catching that slot up on the previous frame's damage.
	RuntimeStoredPixels int64
}
