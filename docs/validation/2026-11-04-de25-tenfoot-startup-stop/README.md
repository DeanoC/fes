# Full native Tenfoot startup; launch procedure stopped

One bounded physical procedure ran on TRWU8CBL with native source e225ea89b,
FPGA producer7808f74c2 and observer98fd021ad. Recovery passed. The two packages,
metadata, selection, UI catalog and experimental FIT passed all seven fixed
writes/readbacks and all seven fresh read-only checks in one owned session.
The matched native boot reached the full Tenfoot App, showing ZX81 and ZX80 in
its serial state stream. Native service generation zero was Ready with empty
live identity, Hold asserted and no reported error.

The procedure **failed before sending the first core-load key**. The staged
operator passed the CPU mapping dictionary as the second argument to
startup.decode, whose second argument is a final-checkpoint name. Its
unknown-final-checkpoint exception stopped the procedure. No load, return,
keyboard, quit, retry, reset or other native follow-up command was sent.

The exact executed helpers, raw capture and receipts were preserved before any
correction. Independent offline analysis verifies the matched cold boot and
all fourteen CPU checkpoints applicable to the selected-service path, through
fat_mount_end. The two later retain_begin/retain_end checkpoints belong to the
fixed-load diagnostic branch, which this mode does not execute. Each of the
fourteen actual checkpoint deletions is rejected. This corrects the API call;
it does not waive a missing checkpoint or turn the procedure into a pass.

The host-only startup_checkpoints helper now binds this mode to the existing
unchanged CPU decoder and exact final phase. Its regression fixture comes from
the actual stopped capture. Eight focused test methods and all six affected
checks pass. The future-operator correction is recorded as a patch; it was not
applied to the frozen failed operator and was not run on hardware.

Independent cleanup passed: original15MHz JTAG readback, exact USB permissions,
private server gone and UART free. The App was last observed idle with SD owned
and active; no core was loaded. The exclusive claim and spare card remain;
factory card was untouched and no factory restore was performed, as requested.

The failed procedure is retained under
/home/deano/fes-retained/de25-tenfoot-stopped-20261104/ with file readback and a
verified observer-source bundle delta. That delta requires the immutable native
source bundle named in stopped-retention.json; combined restoration and strict
fsck pass. There was one physical attempt and no hardware retry.

Full native App startup is now physical diagnostic evidence. Actual App-driven
core launch/return remains unexecuted. Owned HDMI, core input through the USB hub,
ordinary ROM/parts/media admission and native Ethernet remain open. Startup
success does not qualify those DE10-style readiness requirements.
