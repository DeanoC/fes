package menudisplay

// TransientUnderflowCap is one 720p scanline. The runtime reports the
// per-present underflow delta, not the cumulative hardware counter. A
// completion or an available menu status at or below this cap is a tolerated
// glitch; a larger value is a failed scanout. Keep this equal to
// mister::native::kMenuUnderflowPresentCap.
const TransientUnderflowCap uint64 = 1280
