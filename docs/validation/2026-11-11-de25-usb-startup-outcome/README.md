# USB startup failure and bounded Stop outcome

Source `c62d00a86` preserves a typed `usbcontrol.StartupFailure` from an
attempted keyboard/gamepad Open. It records the failing acquisition, driver or
confirmation stage and whether the existing cleanup attempted Stop. Only an
exact successful Stop response supplies a copied inactive ownership receipt,
captured before permanent local retirement. The original error remains reachable
through `errors.Is`/`errors.As`. Formatting and inspecting the record perform no
firmware or driver access. The native application's existing panic now includes
that outcome automatically through the original returned error.

There is no new Stop, reset, retry or query operation. Lost acquisition and
confirmation replies still prohibit a speculative Stop. Missing/malformed/lost
or failed Stop responses yield no stopped receipt. An unsuccessful startup
continues to retain its partial resources and rejects polling, closure replay
and reacquisition through its retired client. Startup failure remains fatal in
the selected native application. Ordinary boot with no input device, hotplug
and replacement ownership still need their own explicit lifecycle; this change
does not implement them or reinterpret every startup error as absent input.

The actual previous native trace stopped while waiting for root connection:
`address=0x440 value=0x1000 mask=0x1 want=0x1 elapsed=500.000768ms`.
The pinned [Linux v6.12 DWC2 register definitions](https://github.com/torvalds/linux/blob/v6.12/drivers/usb/dwc2/hw.h)
identify HPRT power at bit 12 and connection at bit 0. These controller bits do
not establish actual VBUS voltage or a physical cause. No old trace is modified
to invent a Stop outcome. Physical connection confirmation is still pending.

The new disconnected-root regression uses the actual DWC2 startup path and an
independent modeled ownership responder. It reproduces `0x1000`, requires the
unchanged subsecond connection bound, zero device/channel transfers, zero
confirmation calls and exactly one modeled Stop. Later source/driver polling
and closure perform no controller I/O. Separately, tests use the actual C
ownership dispatcher/service/board policies through the existing RPC fixture to
check successful Stop retention, lost acquisition/confirmation/Stop replies,
original-error matching, cancellation after a known acquisition and permanent
retirement. Those are distinct software boundaries, not a combined physical
firmware/USB proof. Normal and race-enabled USB/DWC2 checks pass.

All seven selected software checks pass, including 1,935 parent regressions
and source-bound native firmware/FPGA-model launch integration. The integration
report includes the changed USB source hashes and new startup tests. Receipts
are recorded in `software-result.json`. The old retained SDK is independently
rejected with `native selected source changed: platform/embedded/tamago/usbcontrol/source.go`.
The preparation, source and integration logs are retained at
`/home/deano/fes-retained/de25-usb-startup-outcome-20261111-r1`; archive readback,
independent source restore and strict Git fsck pass. Its declared source-bundle
prerequisites and hashes are in `retention-proof.json`.

No board command, SD write, FPGA programming, native image rebuild,
push, PR, merge or cleanup of old outputs is part of this milestone. The retained
installer candidate is unchanged; its source binding cannot be silently reused
for this changed runtime. Another native trial needs a freshly rebuilt and
admitted SDK/entry/firmware chain. First physical package installation,
independent reboot discovery and DE10-equivalent core-loading readiness remain
open.
