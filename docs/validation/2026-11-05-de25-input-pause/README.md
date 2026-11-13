# Drain USB polling across native core loads

Source `98ce71bb8` connects an optional owned polling-control interface to the
native Tenfoot backend's launch/return operations. Nano typed USB sources expose
that interface through the actual DWC2 driver. Long transitions begin only after
the current USB packet or hub-health operation drains. Any resulting report is
discarded. The device stays enumerated with the same endpoint toggle and secure
owner/generation; no firmware lease mutation or controller reset occurs.

The drain has a 500 ms/4096-step budget. It checks DMA/interrupt ownership,
connection, every admitted channel's idle state and absence of unread RX status.
Paused polls do no register access. Resume clears local pause state and permits
fresh ordinary packet deadlines; it cannot repair a failed or closed source.
Driver failures retire local input and preserve its reservation for checked stop.

The backend holds its existing operation lock across pause, native mutation and
resume. It retries only the source's non-mutating lock-busy response under a
five-second context. A failed pause cannot start another load. Resume failure
parks the exact owned play through a fresh bounded cleanup without resuming SD.
A failed native configuration/return keeps USB paused and reserved. Menu input
requires a fresh neutral report at every paused boundary, including preflight
rejection which leaves the same Ready generation. Sources with no transport
control retain their existing model routing; no physical startup is selected.

The tests join owned USB sources to the actual DWC2 implementation and independent
register/device models. They drain pending keyboard, typed gamepad and split-hub
packets, advance the modeled clock by 100 seconds and resume with correct input,
endpoint toggle and lease identity. Frozen clocks, stuck channels, bad PID,
disconnection, unread RX, untracked channels and DMA changes retire the host.
An owned drain failure retains one checked stop and prevents further polling.
A separate diagnostic test overlay checks 33 ms frontend polling across 48 hub
layout, initial microframe-phase and health-cache combinations. The driver
services split handshakes within a bounded poll; all combinations deliver the
expected report. The overlay source, exact command and log are retained.

The actual Tenfoot App performs three native launch/input/return cycles through
the paused-source backend. Other tests cover preflight rejection, pause/resume
failure before/after launch/return, failed configuration and failed parking or
closure. Normal routing, held-key fences and stale-session cleanup remain tested.
The full native Go race suite, selected parent integration plan and ARM64 USB-only
and serial Tenfoot builds pass; exact receipts and source hashes are in
`result.json`. The serial Tenfoot build still disables USB; the USB image still
selects no FPGA/SD composition. These are separate cross-builds, not a newly
matched boot chain or a physical combined USB/core test.

Evidence and source are retained under
`/home/deano/fes-retained/de25-input-pause-20261105-r1/`. The retention receipt
records archive readback and independent source restoration with prerequisite
bundles and strict Git integrity checks. External toolchains remain dependencies;
no working artifacts are removed. The next hardware prerequisite is the trusted
composed boot selector and native shared-client wiring. Normal package/ROM/media
admission, playable input/video and sustained load/return acceptance remain open.
