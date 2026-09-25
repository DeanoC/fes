# Coleco SGM physical expansion contract

**Status:** Approved 2026-09-25; amended after issue #203 to enlarge the development v2 socket. Diagnostic routes pass timing, but no sealed package or kit acceptance exists.
**Base:** FES `48461fda` plus the uncommitted Coleco SGM work in `out/dev/coleco-sgm-cpu-latest/fes`.
**Owners:** misteross for the shell, module, linker and compiler evidence; FES for later package selection. A change to runtime or FogCast admission belongs to those components in the same integration branch.

## Intended result

A development Coleco package has a normally vacant physical socket. Selecting an Opcode Super Game Module asset links a separate FPGA module at download time. The module presents 32 KiB of SGM RAM, AY-3-8910 ports and sound alongside the console's existing SN76489, with no Python or FPGA compilation on the kit. A vacant socket retains current Coleco behavior. The factory package stays on its current contract until the new shell and module have timed and exact-artifact hardware evidence.

This is the Opcode aftermarket SGM, not Coleco's cancelled original Super Game Module. The [MAME device mapping](https://github.com/mamedev/mame/blob/master/src/devices/bus/coleco/expansion/sgm.cpp) supplies the CPU map and I/O ports; the [General Instrument AY-3-8910 manual](https://map.grauw.nl/resources/sound/generalinstrument_ay-3-8910.pdf) supplies sound-register behavior.

## Measured constraints

The existing `fes.expansion.coleco-bus` 1.0 socket reserves X24–28, Y1–11. Its only M10K column is X26. Eleven M10Ks have less than half the raw capacity needed for 32 KiB (262,144 bits), and the Coleco shell already occupies X26 M10Ks below the rectangle. The full SGM RAM cannot be an independently placed memory array inside this socket.

A throwaway full-shell probe inferred **32** extra `MISTRAL_M10K_TDP` cells for a 32,768×8 registered RAM. The original nextpnr pin (`abce51d34`) placed them outside the slot and kept the slot vacant, but four seed/weight combinations missed system timing. Quartus 17.0.2 fitted the analogous RAM probe at 193/553 RAM blocks and 46.16 MHz at its slow −40 °C corner; the same source without RAM used 163/553 blocks and reached 46.91 MHz. Quartus's worst RAM-probe path was the existing VDP pattern-coordinate to HDMI framebuffer write path. A later rerun of the exact issue fixture with the production router repair (`5dea3ecd`, seed 3 / weight 300) closes system timing at 53.52 MHz and pixel timing at 89.69 MHz while preserving socket vacancy. This resolves the old nextpnr RAM-probe gap; SGM shell and module timing require their own evidence. [Routing issue #184](https://github.com/DeanoC/fes/issues/184) records that investigation. Probe outputs are ignored build artifacts, not package or hardware evidence.

## Chosen decomposition

Keep the version-1 placement rectangle unchanged and put the 32 KiB physical M10K store in the development **shell** as a dormant resource. The independently routed **module** owns the SGM-specific enable registers, address decode, AY register file and sound generation. The module requests shell RAM for its enabled windows; it does not claim that RAM by returning eight data bits over the original response bus. The shell is generic enough to lend this store to another future module, but a selected module must explicitly claim every access.

The version-2 development socket reserves X24–28, Y1–19, with its own half-open CRAM region `(1769,32,2806,1800)`. The repaired nextpnr placer shows that the original 41 usable LABs cannot hold the full SGM cart; 57 usable LABs still fail legal placement. The 73-usable-LAB rectangle passes shell vacancy, shell timing, cart placement and route, cart timing, and CRAM containment at seed 3. This geometry is isolated to v2, so the existing v1 shell and diagnostic remain unchanged. The shell-owned store is an FPGA placement choice; the emulated CPU-visible behavior remains that of the SGM.

## Version 2 socket interface

The 31 request bits (A[15:0], DWR[7:0], MREQ, IORQ, RD, WR, M1, RFSH and reset) keep their order. A new **28-bit** registered response carries:

| Bits | Meaning |
| --- | --- |
| 7:0 | module read data for non-RAM claimed I/O |
| 8 | direct-read claim |
| 9 | WAIT request |
| 10 | maskable INT request |
| 11 | shell-RAM claim for this CPU memory cycle |
| 27:12 | signed 16-bit module PCM sample in the system-clock domain |

The shell accepts bit 11 only for a memory read or write below `0x8000`; it overrides BIOS or console RAM only while the module requests it. Direct-read bit 8 remains limited to non-console I/O and the original `0x2000–0x5fff` peripheral window. Cartridge, VDP and controller reads remain console-owned. A vacant or reset module drives all 28 response bits low. The existing 1.0 socket and diagnostic archive remain valid only with their exact 1.0 shell; the version-2 shell advertises `fes.expansion.coleco-bus` **2.0** and rejects 1.0 archives. No silent reinterpretation of the 11-bit response is permitted.

Shell RAM is registered M10K memory addressed by the Z80 lower 15 address bits. The shell requests its read every system clock and selects the result only when the registered module response asserts RAM claim. A write commits once, at the CPU's established write sampling phase after the request and response socket registers have settled; an SGM claim suppresses the mirrored console-RAM write. The CPU-through-socket simulation must prove that this phase is late enough for both M10K data and claim, including WAIT and back-to-back cycles. Module reset disables both SGM windows without zeroing the RAM.

SGM port `$53` bit 0 enables RAM at `$2000–$7FFF`. Port `$7F` bit 1 clear enables RAM at `$0000–$1FFF` over the BIOS. Port `$50` latches the AY register address, `$51` writes its data, and `$52` reads the selected register. The module implements tone, noise, envelope and mixer behavior for all three AY channels, including the defined reset state. It advances from the 52.224 MHz system clock using a fractional enable for the SGM's 7.15909 MHz ÷ 4 AY clock, then publishes a signed PCM sample; no new clock domain crosses the socket. The shell adds that sample to the existing SN76489 sample with explicit saturation before the 48 kHz I2S transfer. Zero module sample must reproduce existing audio bytes.

## Ownership and integration

- **misteross RTL and producer:** add a distinct version-2 development shell with dormant RAM, widened pinned response FFs and audio mix; build a version-2 SGM module against its exact frozen netlist. Keep the 1.0 shell/diagnostic path intact until the new path has functional and physical evidence. The new module recipe and linker must reject non-socket CRAM changes, changed shell resources, wrong slot version, and unlisted response-boundary changes.
- **Go linker, FogCast and runtime:** admit version 2 as a separate exact shell/asset policy. Preserve the selected module's digest through library, target stage, relink and programmed-RBF identity. No new GP mailbox operation is required.
- **FES:** register the new development package only after timing and link checks; factory image membership is a later explicit decision.

## Acceptance gates

1. Simulate vacant versus selected module, every RAM boundary, `$53`/`$7F` reset and enable transitions, preservation of console RAM under the overlay, direct I/O claims, AY `$50`–`$52` reads/writes, tone/noise/envelope output and mixed I2S samples. Exercise real CPU cycles through both registered socket edges.
2. Synthesize and route the full shell plus independent module with the authenticated Coleco toolchain. Require final structured Fmax ≥52.224 MHz system, ≥74.25 MHz pixel and ≥12.288 MHz audio; verify the socket is vacant before composition and all changed CRAM is within its declared version-2 policy after composition. Do not count `--timing-allow-fail` as acceptance.
3. Compare Quartus as a timing oracle. Address the existing VDP-to-framebuffer critical path if it remains the limiting path, regardless of router outcome. Do not change the functional video result just to relax timing.
4. Only after a sealed exact artifact exists, use the designated kit lease for an SGM diagnostic with visible RAM result and audible AY-plus-SN output. Record shell, module, linked RBF and target identity. Simulation or Quartus fitting alone is not hardware acceptance.

Unsealed compiler diagnostics satisfy the route, timing and CRAM portions of gate 2. A sealed artifact, Quartus comparison and exact-artifact kit acceptance remain pending. This design does not add ADAM, Atari 2600 adapter behavior, bus mastering, video takeover, save persistence or cartridge banking.
