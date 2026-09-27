# Menu framebuffer display

This is a simulation-only native menu scanout implementation. It is not a
registered core, launchable package, sealed RBF, or supported kit display yet.

Run from misteross:

```sh
python3 scripts/sim_fes_menu.py --case reader
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
