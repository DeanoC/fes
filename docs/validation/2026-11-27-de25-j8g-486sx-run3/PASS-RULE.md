# J8G 486SX run 3 — pass rules (written 2026-11-27, before any kit contact)

Artifacts:
- Source: origin/main ebe9d522eee31beb1ddc25be5ea9a6662e08e060.
- Package: fes.pc386 486SX 36540290d89653c33de09e9e3600cc88adb5b316da2442cdc981ccc256b876e1.fcore,
  sha256 870f9cb2b6cd5aaf63d65c27df7f9f8942f24026af0421cecdacedd1268d9189.
  Built at 15c3b748 (the #2344 PR head). Its design BUILD_ID is 296e4bce19f6bebe31d8e12b259e1ed6,
  which equals the 486sx design identity computed at ebe9d522. It is not an exact-main package byte-for-byte.
- Bundle: de25-run3-ebe9d522ee, id 69d7cfdcedd0d247fe5bc72c7b9f016ea88d79f4887452d6bb39f24bc8283518,
  bundle.tar sha256 0ab97aec87f2f2b9fc781ab867fea6e57aaebde10040fd8f4f955a4c8def1be3,
  runtime digest d44560bca37c5db4504bb41fd14d439fc15e52736d29be07d07d63463c421a83 (checked by runtime-digest --head ebe9d522).
- Image: the J8G baseline a1c51ac3, with no flash. #2246's changes since that image are all fabric-side, so they are in the package.
- Disk: pmode b4be6e29 (pre-cached on J8G).

## Item 3 — HPS window path (Launch A, session 1)
PASS only if all of these hold:
 a. the load ack has no error, and its active interfaces include fes.memory.hps-ddr 1.0 and fes.fabric.de25-hps-window 1.0;
    the target reports fes.pc386 running;
 b. the window base and span lie inside the reserved pool 0x85800000..0x957fffff (from the owner/runtime events or the owner's sysfs/debug state,
    never by reading 0x10d21400);
 c. FreeDOS boots from ATA: a prompt frame appears, and the typed `ver` output is visible;
 d. Stop returns {"state":"idle"}, the runtime events show the hold/drain/release sequence, and MENU is back on HDMI;
 e. after Stop the owner reports no window allocated (the pool is free), and dmesg has no arm-smmu fault lines.

## Item 1 — HDMI picture (Launch A, and relaunch B in a fresh session)
PASS only if, for each launch, timed grabs at 10/20/30/45/60 s after the load ack (not wait_stable) include
at least two frames with mean luma > 8/255 and > 1% non-black pixels whose pixel content differs from each other
(the FreeDOS boot advancing, the typed text, and the DOOM menu or gameplay where reached).
An in-session relaunch (Stop, then load in the same lease) is also captured with the same timed grabs. It is recorded
separately as item 1b and does not gate item 1. Both of today's earlier runs showed it all-black (6154-byte PNGs). If it is black again, file an issue with the repro.

## Item 2 — warm reset with a window machine loaded (last, deliberate)
Precondition: items 1 and 3 complete, with the machine Stopped cleanly at least once. Then load the 486SX again, confirm it is running (frame plus status), and
record dmesg and the boot_id. UART recorder (receive-only) is running under UART.lock.
Reset: a single `reboot` over ssh (the same warm-reset path as #2245). No retries.
PASS only if, within 240 s: the same target_id answers on the network; the new boot_id differs;
the UART log and dmesg have no arm-smmu C_BAD_STREAMID / event 0x02 flood (zero such lines); fes-de25-dma (20010200.dma-owner)
probes and binds to its iommu group; the agent reports ready, idle and lease free; and MENU is on HDMI.
Wedge rule: no network at 240 s, or any SMMU flood seen on the UART → stop, keep the claim, ask Deano for a cold power-cycle.
No SysRq, and no second warm reset.

Never read 0x10d21400.
