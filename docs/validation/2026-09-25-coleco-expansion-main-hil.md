# Coleco expansion bus: merged-artifact kit diagnostic

On 2026-09-25, the designated MiSTer Pi accepted the Coleco CPU-bus shell and
diagnostic expansion built from FES main commit
`e3b14506c50e622350d5e3bf00459836467458ae` (PR #181). This is
**exact-artifact hardware diagnostic acceptance** for the named shell, cart,
linked RBF and original probe ROM. It is not acceptance of a new appliance
image or a retail Coleco game.

## Frozen inputs and compiler result

- Kit target ID: `73dc9f5f-1a12-4a95-a820-a9b4e600769a` on the designated
  MiSTer Pi, boot ID `25ecc6a1-16c2-4700-9753-50c62269966d`. The existing
  installed image (`5ec228ad94ef0e01ef82d35737ca516eb817c41be519ca2857ad67801bd8984a`)
  remained selected.
- Sealed development shell package ID:
  `e614e582558cdd7fc19beda938e4e4bfabfbb21ec9a120cccb1e5227c481cbbe`;
  its RBF SHA-256 is
  `b4f0dc8929ffa2ecd9055200c4cdf6cc10a8a8d756381798cbee78230437425a`.
- Diagnostic expansion ID:
  `d3a5723dd841b79013395c0671d266f984c734e436c88d62a481e2a1aea989fc`;
  its archive SHA-256 is
  `af4b289e15c1b6501c1b9136ac57a1c1954cc41a99db9cb9580cd28761709527`.
- The Python producer and Go launch-time linker independently emitted the same
  linked RBF SHA-256,
  `f88455bcb37fdb55f08ba5206de6da5d634fe9ca659af4c760a7cf615e09e04d`,
  with composition ID
  `08c267d208534b4e077dad700479492c38151a9e65a0014b175a0790c9d913c8`.
- The cart's `cram-diff.json` reported `archive_published=true`,
  `route_contract=passed` and **zero** non-ECC CRAM bits outside the Coleco
  socket. The three achieved clocks were 52.315 MHz system, 97.857 MHz pixel
  and 179.953 MHz audio, each above its declared requirement.
- The original bus-probe ROM was 1,145 bytes, SHA-256
  `d6ce9649257df534c1ed9d7078110c25771e8e0254adef0c06698d8e13505f6e`.
  It checks the expansion data register, WAIT-mask completion and IRQ-enable
  readback before jumping to the graphics pass path. The independent graphics
  control ROM was 1,067 bytes, SHA-256
  `f45f692cd3280b235779e676af0b8f6a366e8ad91c9d8f1516487937f4813715`.

## Physical observations

An isolated, private FogCast host built from the same main commit imported the
shell, expansion and probe ROM. Temporary ARM agent and runtime binaries from
that commit were bind-mounted over the designated kit's installed binaries.
Their SHA-256 values were respectively
`9adb8bffcae8f2e1e6538758920d10c45208537fd70e02a6e099d5d93d4fd48a`
and `434a2ccf4032cae4b0845d277fb05a9232b97604ade6b8d636ffae4fff40d02a`.
The host binary SHA-256 was
`4b1dbf1c97b874eac1942393cd6b3e5fec61536e92c80f534c59b0bf616448af`.
The normal library launch acquired the target lease, returned `active` with
`fes.application` 1.0, active `fes.expansion.coleco-bus` 1.0, and the exact
composition and linked-payload hashes above. HDMI showed the expected probe
pass pattern: [linked probe](coleco-expansion-main-2026-09-25/linked-probe.png)
(capture SHA-256
`01982cdd7db82faebdc78c9412031b0a09a34ff8ef262a1e8ea767065cde3ee0`).

Two controls ran on that same shell without an expansion. The bus-probe ROM
stayed black: [unlinked probe](coleco-expansion-main-2026-09-25/unlinked-probe.png)
(SHA-256
`67beb5f0db2df8296e443908102e3f1d36931c43eb802ccfa92210aee7d3f49f`).
The independent graphics ROM rendered its checkerboard:
[graphics control](coleco-expansion-main-2026-09-25/graphics-control.png)
(SHA-256
`ee43ff9b63c3cb6b70f80acc5907c450fb1157ca2609d5fb7eff31b34c37d2b6`).
Each retained PNG is a late frame from a 120-frame ShadowCast 3 capture at
1280×720. A few captured MJPEG packets in the linked run were malformed; the
retained late frame decoded cleanly and showed the complete pattern.

Each launch was followed by a successful host Stop to idle. After the final
Stop, the private host was shut down and removed, its temporary credential
file was deleted, the ARM bind mounts were removed, the original services were
restarted, and the staged target binaries were deleted. Target health returned
ready with the original agent/runtime revision
`6db7e8ff56c1703f790c4897b32cd29ea83726ee`; no temporary executable
mount remained and the kit lease was free.

The probe checks WAIT-mask completion, not its physical duration, and reads
back the IRQ-enable register without independently measuring the IRQ wire.
This run did not rebuild, deploy or qualify the installed appliance image,
physical controllers, audio, or retail cartridge compatibility.
