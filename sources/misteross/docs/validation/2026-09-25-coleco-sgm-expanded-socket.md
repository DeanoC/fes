# Coleco SGM enlarged v2 socket diagnostic

> Dated host-only record (2026-09-25). This is an uncommitted latest-source
> diagnostic, not a sealed package, a build of committed source, or kit evidence.

The [placement investigation](2026-09-25-coleco-sgm-cart-placement.md) found
that the full 888-cell SGM cart cannot legally fit the 41 usable LABs of the
original v2 candidate. A 57-usable-LAB `FES_RESERVED_RECT "24 1 28 15"`
shell kept all 59 edge FFs pinned and the rectangle vacant, but its first
route missed system timing (48.64 versus 52.224 MHz), and the cart failed
legal placement on an AY register FF. The v1 socket remains `24 1 28 11`.

The development v2 candidate now reserves `24 1 28 19`, with 73 usable LABs
after three LABs holding the pinned edge FFs. The separate v2 CRAM contract
is the half-open `(1769,32,2806,1800)` rectangle. The actual cart diff is
bounded by x=1816..2790 and y=88..1621. No v1 geometry or CRAM policy changes.

| Route | Seed / HeAP weight | Final system / pixel / audio Fmax (MHz) | Result |
| --- | --- | --- | --- |
| Vacant v2 shell | 3 / 2000 | 52.9717 / 84.8033 / 165.1255 | 59 exact edge BELs; no other shell cell in reserved placement rectangle |
| Frozen-shell SGM cart | 3 / 300 | 53.2170 / 84.8033 / 165.1255 | 73-LAB placement, GPU route, and all three timing gates pass |

The cart changes 37,781 non-ECC CRAM bits inside the v2 region and zero
outside. All 981 cart cells in the routed JSON have placed BELs in X24–28,
Y1–19. Python `overlay_cram` and Go `expansion.Compose` produced identical
2,800,483-byte diagnostic linked RBFs, SHA-256
`b07e887079f09f9cc96bed6716fae0272b48bb0b0ea73dd05a8a54e1631e88e4`.
The vacant-shell RBF SHA-256 is
`75dfbe286a8e6ffb7c1dabf57e1d9edb4785400c62e6330062b6cb60092cfc79`.
The GPU router logged a provisional 51.99 MHz system shortfall before its
analogue repair pass; the final structured report is 53.22 MHz and passes.

The run used FES base `48461fda` plus the uncommitted SGM diff and the locked
nextpnr `f7370550adb324163ed24e54f7e6756a13569758`, registered-memory
Yosys `e2d425de`, and Mistral `18db2489`. Both routes used the HIP GPU router
on the RX 7900 XTX, not CPU fallback. Diagnostic inputs and reports are under
`sources/misteross/build/coleco-sgm-rect19-probe/` in the worker worktree.
The shell reused that worktree's latest-source synthesized netlist; the cart
reused its independently synthesized SGM netlist. Neither had a production
BUILD_ID, and the producer's clean-source check was not bypassed to publish an
archive.

Quartus Prime Lite 17.0.2 also compiled a diagnostic v2 vacant shell (0 errors,
36 warnings). Its slow 100 C corner reported 49.54 MHz system, 128.04 MHz
pixel, and 231.86 MHz audio; the system clock missed its 52.224 MHz target
with -1.041 ns setup slack. The temporary Quartus project used the socket's
behavioral-register branch because Quartus cannot elaborate nextpnr's
`MISTRAL_FF` BEL instances. It therefore does not preserve the exact boundary
placement used by the authenticated nextpnr result and is a timing comparison,
not signoff for that routed artifact. The project and TimeQuest report remain
under `sources/misteross/build/coleco-sgm-quartus-v2-oracle/`.

Next acceptance work is a sealed clean-source shell and cart build, then
exact-artifact kit testing under the kit lease.
