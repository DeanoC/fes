# Atari ST extended floppy geometry and original demo loaders

This follow-up starts at `779c86d8ee877932d2810fea1bc701fa29c0895b` and
integrates FPGA, shared definitions, runtime and FogCast in producer commit
`4ab1d84b4967394d6f4edf4580e2f7dcb29cf096`. The change is reviewed in
[PR #609](https://github.com/DeanoC/fes/pull/609), stacked on the input/audio
work in #607. [Evidence](2026-10-07-atari-st-floppy-geometry/evidence.json)
binds the bounded results. This is geometry and loader diagnosis, not
fullscreen, border or general demo compatibility acceptance.

## Contract and execution path

The 0.2.0 ST package requires `fes.media.atari-st-floppy-geometry` 1.0,
capability bit 10. It admits exactly twelve raw ST lengths: 80–82 tracks,
one/two heads, nine/ten 512-byte sectors per track. They range from 368,640
to 839,680 bytes. Packages without the explicit extension retain the exact
737,280-byte legacy contract. The shared definition and generated consumers
change together; unrelated computer and expansion interfaces stay unchanged.

Initial library Play and live insertion verify the immutable base's exact
size and, for nonlegacy sizes, matching BPB sector length, total sector count,
sectors per track and heads. The RTL derives physical geometry from committed
upload length, so guest edits to the BPB cannot change WD1772 indexing. The
floppy/DMA reader and writer reject invalid CHS, stop multi-sector transfers
at the end of the physical track and address the expanded SDRAM media buffer
without overlapping the unchanged 512 KiB ST RAM.

Library bindings retain the exact immutable base identity. Complete saved
images use `fes.atari-st-floppy.image` major 1: minor 0 remains the legacy
720 KiB format; minor 1 identifies another admitted geometry. Saved payload
size must match its bound immutable base before transfer. Restored guest BPB
edits are allowed; no saved image is reshaped, padded or silently truncated.
Diskless GEM remains valid. Offline MSA conversion preserves every decoded
byte and geometry; compressed MSA is never delivered to the FPGA.

## Software and FPGA checks

The complete `sim-fes-atari-st` suite passes, including all twelve shapes,
last-sector reads/writes, invalid CHS, multi-sector end handling, full legacy
and maximum-size GP uploads, physical SDRAM modeling and write freeze/drain.
The shared definitions/generated fixtures, focused runtime and host tests,
44 producer Python tests and 25 root helper tests pass. The committed parent
`make check` and `make check-generated` pass.

The selected producer seals shell package
`2b7039a2ce0fe4e16a8a317a8c7c0f5c2d803f9941b38936e915f491c4d0d81d`,
build ID `32b1d68a19c0bf2bb2cadafc00b2976e`, payload SHA-256
`b8fc3119ee602e10cdd4ae03147d9f85c5a620bbd4f6bbc89185d8a36567a4ff`.
Analog signoff at seed 4 passes pixel/system/audio targets with
77.57/54.92/243.19 MHz Fmax.

The parent `core-dev prepare` is nevertheless incomplete: automatic separate
Direct video-part composition fails on frozen request FF output 11. Default
seed 4 reports no path from `WIRE.24.41.FFOUT[22]` to `GOUT.25.42.58`; seed 2
reports the same source with sink `GOUT.25.43.33`. Both exit 125. The part has
28 COMBs in a 36-LAB slot, clock/import checks pass, and read-only inspection
found no occupied frozen endpoint. [nextpnr #165](https://github.com/DeanoC/nextpnr/issues/165)
records the exact compiler pins, input hashes and RAM Tester fixture proposal.
A host-only CPU `router2` comparison with the same seed-4 inputs and strict
fence also exits 125 at the identical source and sink. This is a shared
constrained-routing failure; a GPU-specific fault has not been established. Containment and timing
checks were retained. The shell's built-in Direct is an independent route;
these results do not qualify the new separate Direct or Scanlines parts.

The full default-seed compiler inputs and logs are retained as
`out/dev/atari-st-floppy-geometry/nextpnr-165-seed4-reproducer.tar.gz`, SHA-256
`4be16ffc6c2a85c5f332421184807b05f0668972dbff2e768c57b88353986272`.
The same selected snapshot retains both failed route directories.


The [routing diagnosis](2026-10-07-atari-st-floppy-geometry/routing-diagnosis.json)
uses the pinned Mistral physical mux graph and original scaffold wire ownership.
FF output 22 feeds `GIN.24.41.22`. Without occupancy, an in-fence path exists;
with frozen occupied wires excluded, only 46 physical nodes are reachable and
the destination is unreachable. Removing the physical fence in this graph
allows a path through row 37, changing mux bits below its lower Y bound 3442.
Several immediate exits are held by unrelated shell nets; the short in-fence
path's `V4.24.42.3` is held by `video_request[24]`. Releasing just that one net
in the graph makes the destination reachable. This does not authorize changing
its route or any frozen bits.

The supplied graph probes, ownership exporter and complete diagnostic outputs
are reproducible against the retained scaffold and pinned library. They model
physical mux connectivity and occupied wires, not every nextpnr legality or
timing constraint. A full CPU route without the fence was interrupted after
this diagnosis; it supplies no successful-route evidence. The next compiler
regression should preserve the occupied exits and verify boundary egress when
building the shell, rather than testing an unconstrained standalone RAM design.
Any remedy still needs strict timing, identical boundary placements and zero
outside-rectangle CRAM changes. This follow-up made no hardware transition.


To rerun the graph diagnostic, use a scratch directory, the retained seed-4
`scaffold.json`, and the include/library directories from the pinned compiler
slot. Copy `connectivity-occupied.cpp.txt` to `connectivity.cpp`, then run:

```sh
python3 "$record/export-occupied.py.txt" "$fixture/scaffold.json"
c++ -std=c++17 -O2 -I"$compiler_prefix/include" connectivity.cpp \
  "$compiler_prefix/lib/libmistral.a" -llzma -o connectivity
./connectivity
```

The ownership exporter rejects a scaffold with a different SHA-256. The
unoccupied and single-net-release probes compile the same way. The strict CPU
comparison changes only `--router gpu` to `--router router2` in the issue's
seed-4 invocation and uses fresh output paths.

## Original demo loader captures

| Image | Physical geometry | Raw bytes | SHA-256 |
| --- | --- | ---: | --- |
| BIG original | 80 × 1 × 10 | 409,600 | `608c4beff1780552e8ae6ee5a1efff27198fb6a3ca6274597f1cb528c3970edb` |
| TCB Cuddly | 82 × 2 × 10 | 839,680 | `2069570bdba57ebc5a5c760234b9133496c2f70f9f2d862bc207cbb56579a039` |

The original media sources and complete MSA checks are recorded in the
[earlier admission record](2026-10-07-atari-st-input-audio.md). ROM and demo
binaries remain outside Git. Stock EmuTOS 1.4 US 192 KiB firmware SHA-256 is
`8fbbf8b44fc3e34281eaf8cda5265510e9af9ccda0e3e409111648060d244cfc`.

The new `scripts/sim_atari_st_demo.py` freezes the selected actual FX68K,
EmuTOS, chipset RTL, microcode, C++ helper and generated model. Separate
15-second captures for each disk complete with every frozen input and original
disk/ROM unchanged. Both use producer `4ab1d84b...` and source archive SHA-256
`af247d7c601af1465926b8b348673ca64982301b01c927fd1fbc1fc3200dacf4`.
Capture completion is explicitly distinct from demo compatibility.

EmuTOS detects 524,288 bytes of RAM, sets screen base `$078000` and runs the
CPU. BIG reads one 512-byte boot sector, then leaves a black framebuffer and
records one additional bus fault beyond the four startup RAM-discovery faults.
Cuddly reads two boot sectors and displays an illegal-instruction panic at
`PC=$00007000`, within physical ST RAM. The cause is not established by this
capture; no RAM-capacity requirement is inferred.
Neither reaches its demo screens. The static RAM/palette images and bounded
RAM/disk callbacks do not model physical SDRAM, HDMI, borders or raster effects.

Hatari's [official EmuTOS compatibility record](https://hatari.tuxfamily.org/doc/emutos.txt)
independently lists BIG as broken with bus errors before OS calls and lists
Cuddly among working ST demos. This makes ROM/RAM configuration a useful next
comparison; it does not establish the cause of either FES observation.

## Hardware diagnostic and restoration

The designated Kit A ran derived image
`0beed5b5a6ef41e2967b8d2a500ebea602d205b40562cafcd39e6f5d99448d9f`.
It preserves verified base `efe04797...` and its factory packages, replacing
only the runtime, agent and build-input identity record with selected producer
bytes. The live executables were independently hashed. Runtime SHA-256 is
`91e4f34b2bf91dff6caf68c00b8065eb5ef5748321577b2984f5445f2979ad5c`;
agent SHA-256 is
`b847aabcd54e2dbf3bf6156967dd2a14de0b5c95eeb7aa4ecf2cedbfd3455c96`.
This is a bounded derived-image diagnostic, not a new two-pass factory image
or acceptance of the older installed packages against the revised sources.

Normal library Play linked the exact firmware and used this shell's built-in
Direct, without any unqualified video part. The private matching host/CLI used
an explicitly recorded 90-second upload timeout. The
[diskless capture](2026-10-07-atari-st-floppy-geometry/diskless-GEM.png)
shows GEM with an empty drive A and volatile persistence. Both original disks
were admitted ready under their exact immutable base bindings. Explicit saves
used layout 1.1 and independently verified the record header, core/game/base
identities, payload length, final record checksum and every payload byte.
BIG's 409,600 bytes and Cuddly's 839,680 bytes exactly match their originals,
including the last byte. Stop returned each session to idle with a free lease.

Cuddly was then launched after a cold restart of the same private host/HOME.
Play bound the exact saved revision. Its next complete snapshot and record
hash were identical to the first save. This proves maximum-size upload,
snapshot and restore on the physical memory/mailbox path. The original loaders
only exercise their initial sectors; last-track WD1772 guest reads/writes,
all twelve shapes on hardware, audio, separate video parts and border/raster
behavior are not qualified here.

The [BIG hardware capture](2026-10-07-atari-st-floppy-geometry/big-hardware.png)
is black. The [Cuddly capture](2026-10-07-atari-st-floppy-geometry/cuddly-hardware.png)
and [cold-restored capture](2026-10-07-atari-st-floppy-geometry/cuddly-cold-restored.png)
show the same `$00007000` illegal-instruction panic as simulation. Complete
media comparison does not identify the loader failure's cause. The next
compatibility step is an independent same-ROM/same-disk/512-KiB comparison and
boot-code trace before extending to border effects.

The first private static runtime build crashed when creating a thread. An
isolated ARM thread program reproduces that crash with ordinary static linking
and passes with whole-archive pthread linkage. The corrected runtime above
uses the latter. The unconfirmed first image was recovered to the recorded good
image through a leased maintenance reboot before any guest probe; it is not
hardware-qualified. The first host probe was also stopped prematurely while
waiting on target package inspection; diskless GEM had completed, and no second
guest was active at the stop. The subsequent complete original-disk and cold
restore probes are the successful results above. These private build/operator
corrections do not change production source semantics.

Kit storage initially had only 65,785 KiB free. One unreferenced historical
128-MiB image (`b4281ca7...`) was archived byte-for-byte to the workstation and
hashed before its kit copy was removed. The rejected 160-MiB diagnostic was
likewise archived after fallback. Both manifests remain on the kit. Archive
records bind the full identities and retained paths; factory, good, previous
and every loop-mounted image were excluded from removal. No protected image,
owner configuration, existing save or normal library entry was overwritten.

Final managed rollback confirms the original image
`5032c2d2282da26e79e6f7efc47f9f2a1aa9abca4aad4ca6f19817e82d18ce4a`,
boot ID `acd9603e-9881-444d-8d98-6b6dee554ed4`. The normal host is active,
autostart remains enabled, and its API serves the original 4,253 library entries.
The [populated menu](2026-10-07-atari-st-floppy-geometry/final-populated-menu.png)
was inspected on HDMI. Target health is ready/idle and the lease is free.
The task's private host, exact credential copy and isolated target thread
programs were removed. [Restoration evidence](2026-10-07-atari-st-floppy-geometry/final-restoration.json)
records unchanged owner configuration and the confirmed release identity.

Review/integration still needs the separate video-part routing defect resolved,
then the full selected factory build and two-pass image validation. No PR was
merged by the operator.
