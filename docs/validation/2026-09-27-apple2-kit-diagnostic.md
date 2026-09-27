# FES Apple II: kit diagnostic with linked firmware, four cards and live disks

On 2026-09-27 the designated MiSTer Pi (`192.168.10.84`) ran the `fes.apple2`
package through a FogCast library launch. The launch linked the open
diagnostic firmware and one probe card in each of sockets 2, 4, 5 and 7. The
full HDMI-checked sequence passed on hardware, including a live disk insert,
boot, eject and swap. The first runs found a card defect that simulation could
not show; it was fixed and the fixed cards passed.

This is a **hardware diagnostic**. The package, firmware, cards and disk are
exact artifacts. The runtime and agent were diagnostic builds of FES
`620417b7`, taken from a `make dev` image and temporarily bind-mounted over the
kit's installed ones. It is not image or appliance acceptance.

## Setup

- Kit lease: free before and after. The isolated FogCast host (`fogcast-api`
  from `620417b7`, private home with a minimal config) claimed it at launch.
  Stop released it. No other host ran against the kit.
- Kit binaries: runtime `dd6952e98618817f637b07d2f89b7412dd3909c773548c29c5aeeacfc58945a6`
  and agent `afe0a455346f862cc158b0dce7a4a1ee1ddcb9de334e6e60b523edf62f92cf63`.
  Both were extracted from `out/native-integration-dev/development/linux.img`
  (FES `620417b7`), streamed to `/tmp`, bind-mounted over `/usr/sbin`, and
  restarted through their init scripts with the lease free. Afterwards the
  bind mounts were removed and the image's runtime `d5776191…` and agent
  `d9d2cf5a…` (revision `e6a85546`) were restarted. The installed image was
  never modified.
- Package `e02ec04222cefcc6dbe3094cf5eefc28aa3609bbea32b4dbe3bb64b70ecd77e5`
  (archive `33779b25…`, shell `core.rbf` `80ba0cd8…`; see the
  [seal record](2026-09-26-apple2-pathfinder-seal.md)).
- Disk: the open diagnostic image, 143,360 bytes, `d7033489…`.
- Verification: ShadowCast 3 capture at 1280×720 YUYV. Text screens are decoded
  cell by cell with the core's font and compared with the expected screen
  memory. Lores, hires and mixed are compared with the independent reference
  renderer. Keys are USB HID usages sent through `POST /api/v1/session/input/event`.

## Result (final run)

Firmware `d97a495d0a047d738c40b34e116331381d640e3cc63a1b9ecd3184a3d95997c5`
(adds the `D` slot dump). Fixed probe cards:

| Slot | Expansion ID |
| --- | --- |
| 2 | `02bc5110819225d48d19a1c86d95ff206c160c8db4f0273f902fa4514ceb9331` |
| 4 | `2cb80a6e0d1d0458184f820dfe7727cd5a0c246de87e5456cb42c7c40ed5298d` |
| 5 | `2c78712f5eff832d847683f7b6d45872df42634196b63d471dbda7d54fee139d` |
| 7 | `66adfb220ee06c988ec0aa0c4d96125d0e827709435f1bd07ae4a76bd60df9d2` |

The session reported composition
`fa98c0575fb9fc5a0ced353bed235ac534e6e94251b6d62b23c56554b7f00a33` and
programmed RBF `d994d1f33656000c4d7e243ea7450b7508a00287425f1b7d9dfec574883a79bc`.
Both are identical to an offline `fes-slot-link` composition of the same inputs.

| Step | Result |
| --- | --- |
| Launch, `fpga_native`, drive empty | active, input attached |
| Boot self tests (RAM, language card) and text screen | 0 of 960 cells differ |
| `L` / `H` / `M` lores, hires, mixed | mean abs 8.6 / 7.5 / 7.2, no strongly differing pixels |
| `T` text, HID echo `QUICK` + Shift-1 | 0 cells differ; `QUICK!` echoed |
| `S` slot scan | `PROBE CARD OK IN SLOT 2/4/5/7` |
| `D` slot dump | exact; each card `FESPROBE`, ID `A2`, scratch `5A`, `$C800` `C33C` |
| `change-disk`, `B` | unit `ready`; `DISK BOOT OK` |
| `eject-disk` | unit `empty`; machine still running |
| Control+F12 | self tests and banner again, 0 cells differ |
| `change-disk`, `B` again (swap) | `DISK BOOT OK` |
| Stop | idle, lease free |

Audio captured from the HDMI sink shows transients aligned with the key echoes.
The firmware's click toggles the speaker twice about 4 µs apart, far shorter
than one 48 kHz sample, so this shows audio activity, not a tone test. Decoded
screens, three captures and the run log are in
[apple2-kit-2026-09-27](apple2-kit-2026-09-27/).

## Defect found and fixed

With the first probe cards (`623cfde6…`, `f00ff82d…`, `63091919…`, `f2558ac4…`)
every step passed except the slot scan, which printed nothing. A late-bound
firmware dump showed each card answering its signature, ID and scratch
register, but reading its `$C800` RAM back as `00 00`
([before-fix-dump.txt](apple2-kit-2026-09-27/before-fix-dump.txt)). The probe
card's self-check therefore failed silently.

Cause: synthesis mapped the card's inferred RAM as a dual-clock
`MISTRAL_M10K`. The nextpnr cart merge (`mistral/fes_slot.cc`, nextpnr
`a93fe013`) drops the card's clock buffer and reconnects `CLK1` of
`MISTRAL_M10K` (and both clocks of `MISTRAL_M10K_TDP`), but not the `CLK2` read
clock of a dual-clock `MISTRAL_M10K`. That clock stayed on an undriven net.
Timing, CRAM-fence and RTL simulation checks cannot see this. For a single card
the composed RBF was byte-identical to nextpnr's own full-design RBF, so the
composer was not at fault.

Fix: FES `4f103479` makes the probe RAM an explicit single-clock M10K. The card
producer now rejects any card clock pin not on `system_clock.clocks[0]`; the old
cards fail that check at the exact pin. FES `0c88c377` adds the `D` command,
with a simulation check that includes the 6502 `STA abs,X` dummy read and the
`$C800` ownership the scan leaves behind. The nextpnr fork should still
reconnect `CLK2` for dual-clock `MISTRAL_M10K` cart cells. That is a
toolchain-lock change and needs a shell reseal.

## Not established

Exact-image or appliance acceptance of a new runtime/agent, any Apple ROM or
commercial software, disk writes, a probe-card tone, controller ports, and
long-duration stability.
