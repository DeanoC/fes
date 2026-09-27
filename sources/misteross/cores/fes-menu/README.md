# Menu framebuffer display

This is a simulation-only native menu scanout implementation. It is not a
registered core, launchable package, sealed RBF, or supported kit display yet.

Run from misteross:

```sh
python3 scripts/sim_fes_menu.py
```

The read-only 128-bit Avalon reader fetches exactly 921,600 XRGB8888 pixels
from one of two 4 MiB slots. It bounds each burst to its frame, reserves FIFO
space for every response and keeps one burst outstanding. Cancellation drains
issued reads and discards their data before the reader reports idle. `rst`
here requests that drain; it must not destroy the outstanding-transfer count.

`WINDOW_BASE` is supplied by the shared HPS DDR contract when board integration
is added. Its default zero disables starts. Simulation supplies the proposed
window solely to verify addresses. The reader does not allocate Linux memory
or expose an arbitrary read address. Shared DDR RTL, boot port layout and
runtime admission are the separate in-progress HPS DDR dependency.

The video controller uses fixed 1650x750 timing at the intended 74.25 MHz
pixel clock. It prefetches in vertical blank, switches a pending slot only
at a frame boundary, and acknowledges its sequence only once its initial
pixels are available. One submission may be pending. A frame that cannot
start safely is black and its submission remains pending for a later frame.
During an active frame, missing pixels are black and counted; late pixels
are discarded by raster index rather than shifting following pixels.

The video simulation checks complete frame timing and pixel data, blocked
submissions, deliberate underflow, delayed-data recovery, reset/drain, pending
switch cancellation and counter saturation. It writes `slot0.ppm` and
`slot1.ppm` under `build/sim/fes-menu-video/` for inspection. These modeled
results do not establish kit DDR bandwidth or routed clock closure.
