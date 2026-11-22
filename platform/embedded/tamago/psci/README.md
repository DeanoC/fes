# Bare-metal ARM PSCI & Hardware Entropy

`platform/embedded/tamago/psci` provides ARM PSCI (DEN0022D/E) system power
control (`SYSTEM_OFF`, `SYSTEM_RESET`, `SYSTEM_RESET2`, `PSCI_VERSION`,
`PSCI_FEATURES`) and ARM SMCCC TRNG (DEN0098 v1.0) / ARMv8.5-RNG (`RNDR`)
hardware entropy support for bare-metal TamaGo on DE25-Nano and QEMU `virt`.

## Conduits

- **DE25-Nano (`board/de25nano`):** uses `SMC #0` into Agilex 5 EL3 TF-A BL31.
- **QEMU `virt` (`board/qemuvirt`):** uses `HVC #0` when booted directly at EL1
  (`-kernel`, `ID_AA64PFR0_EL1.EL2 == 0`). When booted through the EL2 entry
  shim (`virtualization=on`, where `entry.S` sets `HCR_EL2.HCD=1` to disable
  `HVC`), `qemuvirt.PSCI()` returns a disabled client whose calls safely return
  `psci.ErrUnsupported` without trapping.

## Runtime Entropy (`runtime/goos.InitRNG` / `GetRandomData`)

`InitHardwareEntropy` and `FillHardwareEntropy` are `//go:nosplit`, zero-allocation
helpers wired into `board/de25nano` and `board/qemuvirt` in place of the former
hardcoded xorshift PRNG. Sources are probed in preference order:

1. **SMCCC TRNG 64-bit (`TRNG_RND64`, `0xc4000053`)** — up to 192 bits/call,
   extracted per DEN0098 §2.5.2 (`X3`, `X2`, `X1`).
2. **SMCCC TRNG 32-bit (`TRNG_RND32`, `0x84000053`)** — up to 96 bits/call
   (`W3`, `W2`, `W1`).
3. **ARMv8.5-RNG (`FEAT_RNG`, `MRS RNDR`)** — checked via `ID_AA64ISAR0_EL1[63:60]`.
4. **Counter-mixed fallback (`cntpct-fallback`)** — `xoshiro256**` seeded and
   re-mixed from `CNTPCT_EL0`. `EntropySource.IsHardware()` returns `false` for
   this fallback so callers and probes never mistake it for a cryptographic TRNG.
