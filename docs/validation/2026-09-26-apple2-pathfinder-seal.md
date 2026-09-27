# FES Apple II pathfinder: simulation, sealed shell and slot composition

On 2026-09-26 the `fes.apple2` 0.1.0 pathfinder passed its host simulations,
sealed a multi-socket shell with the HIP/nextpnr producer, sealed an open probe
card for each physical socket and composed them with the shared Go linker. This
record is **host simulation, sealed build and host-side composition evidence**
only. No kit, runtime-on-target, HDMI, audio or FogCast session test ran, and
nothing here is hardware acceptance.

## Scope

- ABI `fes.computer` 1.0 (mister-packages), with its golden exchanges copied to
  the runtime and misteross consumers.
- Machine: 6502 at 1.0205 MHz, 64 KiB RAM with language card, text/lores/hires,
  speaker, HID keyboard adapter, built-in Disk II in slot 6 reading a
  live-swappable 143,360-byte DOS-order image (read-only).
- Late-bound firmware: one exact 16,384-byte `apple2-firmware` ROM covering
  `$C000-$FFFF`, linked into 16 blank M10K lanes before download. Only the open
  diagnostic firmware was used; no Apple ROM is shipped or tested.
- Slots: physical sockets 2, 4, 5 and 7 (`fes.apple2-bus.slots/1`, optional
  interface `fes.expansion.apple2-bus` 1.0). Slots 1 and 3 are vacant.

## Simulation

`make -C sources/misteross sim-fes-apple2` passed:

- `sim-fes-apple2-mailbox`: the `fes.computer` mailbox replays both golden
  scenarios (34 and 624 exchanges).
- `sim-fes-apple2-machine`: diagnostic firmware self-test (RAM, language card),
  text/lores/hires/mixed frames byte-identical to the independent Python
  renderer (`diagnostic/render.py`), open probe cards in sockets 4 and 7 found
  by the slot scan, disk boot and sector reads.
- `sim-fes-apple2-board`: the full top through the real mailbox: HID key
  presses, live disk insert, boot from the inserted image, live eject and
  Ctrl-F12 warm reset.

## Sealed shell

- Sealed at FES `cd4b1633dc7286d850a0a47dd2284f200baabbfe` from a clean tree
  with `toolchains/apple2.lock` (Yosys `e2d425de`, Mistral `7ed06e21`, nextpnr
  `a93fe013`). Package ID
  `f248062558e38b18266e870b0284743e19524df7f8ab3a35db9cfd3aa6cb531c`, BUILD_ID
  `0261aaf0d14e622ab6381dd7c5b91671`, archive SHA-256
  `20c6cc5afed27f93d06971a8a90b84b82ecec3b77cfd41c6a194b373c0293109`.
- `core.rbf` 2,834,256 bytes, SHA-256
  `80ba0cd8843bbfb3ce352b24433825fed441cf37bec3f47dea1234e94a506c6a`; ROM map
  SHA-256 `2c9e4e2a4b07fa45a39e03c4cdeb1043feaa8501887a09c7c3ce92aaa61a8151`.
- Final clocks (seed 5, weight 2000): system **55.35** / pixel **97.88** /
  audio **161.32 MHz** against **52.224 / 74.25 / 12.288 MHz**. 5,080/83,820
  combinational cells (6%), 2,071 flip-flops, 221/553 M10K. 372 pinned socket
  cells: per socket 32 request and 28 response flip-flops plus 33 per-row clock
  anchors.
- `make core-dev CORE_DEV_ARGS='prepare --core fes.apple2 ...'` at parent
  `ece829a544a695e8f5ea023c1d21461d3aaa94f3` rebuilt the shell from the committed
  tree through the registered recipe. It produced a byte-identical `core.rbf`
  under package ID
  `e02ec04222cefcc6dbe3094cf5eefc28aa3609bbea32b4dbe3bb64b70ecd77e5`
  (archive SHA-256
  `33779b257a9c1a5eac15fd289e1eec1d936b936b3859575f48a3f3f82823360e`), now in
  the shared artifact cache.

## Slot cards and composition

Probe cards for shell `f2480625…` (each route passed at the shell clocks; every
changed CRAM bit lies inside that socket's rectangle, zero outside):

| Slot | Expansion ID | CRAM bits inside |
| --- | --- | --- |
| 2 | `fc21efebea670ba5ff09aff358a620986b67458da607ffd4f5fbe494f04d1409` | 28,534 |
| 4 | `3e22f61110cc01f1e14996654816d0b5f34fc74bfa68c63d03b23c899ad9a1f7` | 28,426 |
| 5 | `8c87a24273a2fe40972769a0e870e439ee42b4857725f9499868e0682c35cfe0` | 29,025 |
| 7 | `59526fa02ea2c99f1b8db141c24f3133669ac0f28879f64894da6e51dab20d6b` | 28,415 |

`expansion/cmd/fes-slot-link` composed all four cards and the diagnostic
firmware (SHA-256
`d2f093e6f7ce5b16f9fd6ac1f27b9e8fd183ac87fb78a42969a93a9e8c071129`) in 0.6 s:
composition ID
`68277815740cbd6b52e5a3ec712fff04d714d366c48171e01aa35f4a28ff012e`, linked
payload 2,880,238 bytes, programmed SHA-256
`4b62d6d8cfb7b878e0d8d8165a0915cf73240683e2ac87689b30c50a77fe3efb`. A two-card
subset composed; a second card for one slot was rejected.

Cards bind to the shell package ID. The card producer resealed all four probe
cards against `e02ec042…` (same BUILD_ID; expansion IDs
`623cfde6fa624aecb05b0ef4f5a7a0264d7a7e34bf2f5da70da89a59fbb0b1b7`,
`f00ff82d17e911c879f5523df9948d95b4ce200399445c8705d712d6f1d8968b`,
`630919194c61220ad1cba13014d86f12cbe5ab7e71ca54d204c58017138c0bbb`,
`f2558ac44d7b07ee8f1de286597905b277c14cb167bf55433b31bd5f6fd9e817` for slots
2, 4, 5, 7). The CRAM counts were identical. Their composition
`e0c91c3cf9f5a49c12236886751cd68a33e69ae90bc5f91f16e3f9815043f62e` produced
the same linked payload and a byte-identical programmed RBF (`4b62d6d8…`).

## Not established

Kit programming, HDMI picture, audio, keyboard or disk behavior on hardware; the
runtime and FogCast agent on the target; any Apple ROM or commercial disk; disk
writes, a second drive or tape. Kit work needs the designated kit, the
kit-sharing lease and explicit authorization.
