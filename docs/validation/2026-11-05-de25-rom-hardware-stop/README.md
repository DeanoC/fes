# First retained-ROM hardware trial stopped before Run

The changed native source `145345b675cbc4d27df613db0bca4ddce51fafba` rebuilds
the SDK, entry, BL31 and U-Boot/SPL, with a new matched file composition. Its
actual linked paths and strong firmware providers pass inspection. The original
format-three FPGA package remains unchanged. The actual upload catalog selects
the captured ZX81 ROM and an 8 KiB `JP 0000` diagnostic with distinct activation
receipts. Both choices pass retained-package preflight. The diagnostic executes
in the actual CPU/ROM/board model for both ROM sizes, continuously fetching at
address zero, with the existing fault, held-return and reset gates intact.

The bounded operator passes its positive result and 40 damaged-result checks,
86 actual upload-runner cases, 357 damaged-receipt checks, read-only transport
checks and receive-only fragmented failure-tail checks. The complete read-only
source/candidate/fixture/claim/UART/USB preflight passed before the trial.
The first native call-site inspection omitted the cross-tool library path; its
failed preparation log is retained and the corrected inspection passed.

One physical attempt used TRWU8CBL. Qualified recovery boot, all eight fixed
Ethernet uploads and eight fresh SD hashes passed, including protected boot-file,
card, PHY and ownership checks. Matched native startup completed all 14 CPU
checkpoints and reached Tenfoot Ready with the shared USB input selected.

The first ZX81-ROM launch completed 1,658,880 FPGA bytes with zero pending bytes
and a complete held-data activation at configuration generation 4. CONFIG_STATUS
reported ready; the read-only SMMU and DDR-firewall checks also returned success.
Control opening then returned an unrecognized bridge firmware tag. The runtime
reported `reboot_required`, native generation 1, cached `Held: true`, and the
selected ROM receipt. No Run/fetch observation, ROM completion, second launch,
return, USB resume or normal-load acceptance was observed. The procedure stopped
at this first failure, retained the panic tail and issued no retry.

Independent cleanup confirms the exact original USB permissions, recorded 15 MHz
clock readback, stopped private JTAG service and free UART. The factory card and
other kit were untouched. The continuing claim and test card remain in place;
no factory restore or power cycle was performed. Cached Held status is not a
physical CPU-halt proof. Exact artifacts, operator sources, logs and source
bundles are retained at `/home/deano/fes-retained/de25-rom-hardware-stop-20261105-r1/`.

The internal ROM failure cause is unproved: this firmware does not emit its
cached ROM failure stage, first error or access/timer observations. The next
step adds that diagnostic evidence without new MMIO, relaxed limits, a reply-tag
bypass or replaying this failed image. ROM hardware acceptance and full
DE10-equivalent core loading remain open.
