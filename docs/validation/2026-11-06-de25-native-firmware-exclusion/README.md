# Native CPU exclusion for private firmware RAM

The Nano startup now removes `[0x50000,0x60000)` from its EL1 page tables before
its first `go_hwinit` checkpoint. This completes the native CPU side of the
candidate memory boundary. It does not reserve that physical RAM for BL31 or
enable the separate on-chip firmware layout.

The allocation-free ARM routine runs on the primary CPU before devices or Go
package initialization. It consumes its attempt, checks the pinned CPU state,
read/write WB translations of the table pages, low table pointers and all
sixteen original Device descriptors. Only then does it clear those sixteen
PTEs, clean them, invalidate the EL1 TLB and require read/write level-3
translation faults at both ends of every page. It checks CPU state again and
restores PAR_EL1. It has no stack frame or calls and never reads or writes the
excluded target memory. A failed Nano startup halts; there is no repair/retry.

Before device mapping, the board's mapper independently requires the invalid
descriptors and actual translation faults. Its complete table-tree audit also
requires identity leaves with two matching samples, rejecting other virtual
addresses that alias the region. Every subsequent mapper observation, including
both sides of DMA publication, includes the negative mapping. Uncertain or
restored access retires the mapper and retains resources. The initial attribute
audit accepts only these selected sixteen holes and the existing null trap;
other missing pages still reject. The read-only `Inaccessible` kind accepts
only the complete fixed candidate, and supplies no memory or activation grant.

The SDK and entry CPU-resource contract is version 2. It explicitly names
`firmware_base=0x50000` and `firmware_bytes=65536` as an exclusion. The native
CPU-mapping record includes them and its observation byte count includes the
excluded bytes. Current receipt/trace consumers reject the older contract,
missing fields, changed bounds, or incomplete observations. SPL's exclusion
range and the Go/assembly range have an explicit consistency check. No FPGA
producer, firmware ROM transport, 16-byte frame, one-second deadline or 64-poll
bound changed.

Validation on implementation commit
`31d716957759c3dede3330877dac2ce7bcd2d643`:

- Race-enabled host tests passed for the mapper and native fabric/SD/USB
  integration. Cases exercise every page boundary and read/write probe, stale
  successful translations, permission/external/stage-2 faults, table-read
  failures, state drift, aliases at every level, replay, caller-slice ownership,
  and exclusion loss after DMA publication. Uncertain changes cannot report
  success or retry.
- Thirteen fresh QEMU boots executed the actual ARM routine: success, replay,
  wrong/misaligned RAM, late use, first/last bad PTE, bad root/low table, and four
  alias cases. Success also completed real DMA mapping and positive/negative
  translation checks. QEMU is not Nano hardware evidence.
- The real selected-ROM, shared-gamepad Tenfoot Nano application cross-built,
  and its compressed entry built at 7,516,160 bytes. The entry binds the exact
  SDK receipt and full decoded payload. All 2,098 selected SDK/entry sources
  were archived and independently read back.
- The actual stripped Nano ELF was audited using retained Go function metadata
  and GNU ARM disassembly, without changing its build flags. Its startup calls
  the exclusion routine before the first checkpoint. The routine is a
  zero-frame leaf, and compiled stores are limited to its attempt latch, the
  sixteen owned PTEs and the ABI return slot.
- All seven required changed-input integration steps passed, including 1,500
  parent regressions, policy/whitespace checks, generated-consumer consistency
  and native firmware/FPGA lifecycle integration. The focused CPU resource and
  trace tests also reject omitted exclusions and the old receipt version.

The [receipt](result.json) binds the build, source snapshot, instruction audit
and software evidence under `out/nano-firmware-exclusion-20261106`. Development
failure logs are retained, including an early trace fixture missing the new
field and a host audit that hit its 90-second cap before producing a cold
profile. The completed native audit checks the exact SDK/entry/payload/source
bindings and instructions directly. It does not claim a complete cold firmware
chain; the audit and runtime bounds were not widened.

The current firmware ELF gate still rejects the private OCRAM NOBITS prototype.
No matched BL31/SPL image was constructed or hardware-tested, and no board,
SD card, JTAG, flash or network upload operation occurred. TRWU8CBL's claim and
spare test card remain retained after the preceding O2 ROM stop; its factory
card and the other kit are untouched. Earlier physical input, boot or core
results remain scoped to their original artifacts.

Next implement the secure firmware's reservation check before its first BSS
clear and stack selection, complete the OCRAM-user lifetime contract, then
admit only the matched selected ELF layout and build a fresh bounded trial.
Physical latency, ROM completion/Run/return, ordinary package admission and
DE10-equivalent core loading remain open.
