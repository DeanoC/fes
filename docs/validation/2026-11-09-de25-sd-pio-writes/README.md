# Native owned SD PIO writes (software only)

The SDHCI driver supports explicit `Config.AllowWrites` single-sector CMD24
writes. `sdcontrol.Open` now requires trusted `ReadOnly` or `ReadWrite` selection;
the Nano board explicitly remains read-only. No firmware wire contract, board
profile, DMA mode, voltage, bus width or FPGA artifact is changed.

All eight affected checks pass, together with race-enabled driver/session/FAT
tests. Coverage includes exact data and sector order, independent card-ready
observations, protected/default-read-only refusal, uncertain completion after
possible commit, permanent retirement/no retry, concurrent quiescence between
sectors, same-card resume and actual C firmware lease containment. A real FAT32
settings update survives remount and independent `mtype` readback.

The first joined failure case exposed an overbroad controller-model reset:
CMD/DAT reset incorrectly invalidated host/PHY preparation. The corrected model
invalidates preparation only on reset-all; the original failed log is retained.

Native ARM64 cross-build is pending. The write-protect input and actual
write/readback/stop/resume need hardware qualification before a board image
selects writes. Previous U-Boot upload/readback evidence does not qualify this
Go write path. FAT still updates only existing preallocated files; file allocation
and general native package installation remain open. Physical keyboard/display
checks remain deferred until the operator returns. No hardware was touched.
