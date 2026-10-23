# 2026-10-23: ADV7513 input clock delay of −0.8 ns on kit A

Record for #1003. Every core forwards `HDMI_TX_CLK = pixel_clk` on the edge
that launches the pixel bus. The runtime set the ADV7513 input clock delay
(`0xBA[7:5]`) to none, which left the transmitter sampling at the edge of its
window. The runtime now programs −0.8 ns (`0x20`).

## Why −0.8 ns

The `fes.spectrum-plus3` 0.2.0 archive `2371a801…` (#1001) lost the kit A HDMI
link about once a second. Run `c997e` (2026-10-22, recorded in #1001's
[Spectrum tape record](https://github.com/DeanoC/fes/blob/e5bd594a8de4fe9a67523beeb1253950f38feebb/docs/validation/2026-10-22-spectrum-tape-ddr.md#kit-a-check)
and [on #1003](https://github.com/DeanoC/fes/issues/1003#issuecomment-6349192960))
swept the delay during one session of that archive, 20 s of capture per setting:

| `0xBA` | Delay | Link drops in 20 s |
| --- | --- | --- |
| `0x60` | none | 14, and 15 when repeated last |
| `0x40`, `0x20`, `0x00` | −0.4, −0.8, −1.2 ns | 0 |
| `0x80`, `0xa0` | +0.4, +0.8 ns | 0 |

In SDF from the locked nextpnr, the bus reaches the pads 1.45–3.3 ns after the
clock on every Spectrum model, so sampling earlier moves away from the bus
transition. −0.8 ns is the middle of the negative settings.

## Kit A check

Run `c1003b` (08:53–09:08 EEST). Kit software was built from this branch
(`27d131a5`) merged locally with #1001 (`e5bd594a`), whose runtime admits the
Spectrum tape 2.0 archives, on kit A's running image `88e85ec5`. It was
installed for the run only, and the baseline software `999a6ea5` (previous
`570515b5`) was restored after it. Image fingerprints before and after match,
and the kit was released idle on its recorded baseline. No register was
written by the test; it read `0xBA` over I2C after each launch.

Each line is a 30 s (900-frame) capture taken 10–12 s after launch:

| Session | Archive | `0xBA` read | Link drops |
| --- | --- | --- | --- |
| Idle display | — | `0x20` | 0 |
| `fes.spectrum-plus3` 0.2.0 | `2371a801…` | `0x20` | 0 |
| `fes.spectrum` 0.4.0 | `d813d96e…` | `0x20` | 0 |
| `fes.spectrum128` 0.2.0 | `e9215941…` | `0x20` | 0 |
| `fes.pentagon` 0.2.0 | `128f8548…` | `0x20` | 0 |
| `fes.zx81` 1.6.0 | `81aac175…` | `0x20` | 0 |
| `fes.c64` 0.4.0 | `139d53f8…` | `0x20` | 0 |
| `fes.spectrum-plus3` 0.2.0 again | `2371a801…` | `0x20` | 0 |

A drop shows as a run of about 32 black frames. Only each capture's first
frame was black. The ZX81 draws a black screen, so it was judged by its `K`
cursor instead, which is present in every frame but the first.

## Not established

- Kit B, and cores not run here: Apple II, Atari ST, Coleco, SMS, SG-1000,
  Pong, RISC-V, RAM tester and PC/XT packages, and the menu core in a session.
- Margin: no core was swept at −0.8 ns to find its own window.
