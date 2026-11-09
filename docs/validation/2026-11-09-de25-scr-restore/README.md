# Restore the vendor startup helper's caller state

The [first actual MMU candidate](../2026-11-09-de25-mmu-probe-mode-trial/hardware-result.json)
reached its late startup hook with `SCR_EL3=0x239`. Its provider and cold CPU-mode
checks failed before any MMU register write. Recovery boot, thirteen network
uploads/fresh SD reads and native image programming passed; the launcher and
tape transfer did not run. The consumed candidate and host cleanup are retained.

The pinned vendor `bl31_plat_arch_setup()` calls `invalidate_cache_low_el()`.
That helper selects SCR.NS for lower-EL TLB invalidation, but omits both caller
SCR restoration and RET. The original linked function is exactly 32 bytes,
ending in DSB and immediately followed by `asm_dcache_level`. Its control-bit
leak matches the observed rejection. This is a startup helper defect; the cold
and runtime CPU/ownership guards remain unchanged.

The selected MMU producer now pins the complete vendor cache source SHA-256
`f41d7e66d603aa9fb327bcbf653660592e865825f120e9a419abee87b339e360`.
It preserves all original invalidation instructions, then restores the saved
SCR, executes ISB and returns. Only the explicit selected MMU profile installs
this repair. Unselected recovery/default source transformations are unchanged.
No guard, ownership observation, timeout or cache permission is weakened.

Runtime policy format 2 includes `restore-caller-scr-and-return-v1` in its cold
chain identity. The final ELF verifier requires the actual global helper's 44
bytes to match all eleven reviewed instructions inside executable firmware.
Boot trace selection requires that linked helper proof and the new policy
digest. Stale, missing or changed helper proofs reject before loader admission.

An isolated QEMU A53 guest executes the actual extracted instructions at EL3.
The original control reproduces the SCR leak and fall-through. The repair
restores exact caller values `0x238` and `0x239` and returns normally. Separate
partial repairs fail with the expected SCR mismatch or fall-through error.
The linked repaired fixture matches the producer's exact 44-byte machine code.
This is helper semantics on an emulated CPU, not Nano MMU/cache qualification.

The portable MMU fixture also reproduces the observed startup registers.
The leaked `0x239` cold state retires the object permanently; changing its SCR
afterward does not recover it. A fresh object with restored `0x238` passes the
unchanged guard and transition, then permits the existing active nonsecure
context. Missing-enable, partial-write and ownership failures remain denied.

The [check closure](checks.json) passes 1,776 parent tests with three existing
skips, 27 focused tests, generated consumers and the selected native/producer/
firmware integrations. The FPGA sweep retained its completed 211-module prefix
at the original 300-second cap; the last three modules pass separately under
the same per-module cap, covering all 214 software modules. This is not FPGA
timing or hardware acceptance. Actual ARM ELF mutations removing an original
invalidation, SCR restoration or RET each reject at the new helper verifier.
MMU/CPU/ownership/tape implementation bytes and the one-second deadline are
unchanged. The unselected qualified recovery still passes current source checks.

The matched BL31/SPL/FIT/SOF build passes. BL31 remains 274,757 bytes, with
translation root `0x8003d000`, private witness `0x5e3c0` and repaired helper at
`0x80023bd4`. The unchanged native entry is independently receipt-verified.
Native boot ID is `7efa0304ddd339aa`; the sealed FPGA core payload is unchanged.

| Artifact | SHA-256 |
| --- | --- |
| SOF | `b64ecaf6294e1673a3f65232f8296ed8ec6983ae5134921e41a2ff394d9f2833` |
| FIT | `65a35515fe8f6eea5466771882565e3ae0ebc858003400c672f8a1f78f397f4a` |
| SPL hex | `abb08ab940b8704ee511ffd02e6f7c4ee667dc69929e5d5231e3252cf94f9a34` |
| Native entry | `edeb06b1c75e2549f432a8152d4ee6f72c883e72c25c524f462d3f540764e76f` |

Local evidence is `out/nano-mmu-scr-restore-20261109`. The new trial's complete
file-only admission and operator models pass, but it still requires a frozen
procedure and new bounded physical proof. Candidate `e4ecad3bc8a33357` is retired
and must not be retried. Ordinary core-load acceptance remains open.
