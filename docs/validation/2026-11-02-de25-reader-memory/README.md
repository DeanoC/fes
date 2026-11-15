# Nano cold DDR reader memory reservation

The cold diagnostic now binds the sealed DDR package to a fixed 8 KiB test
window and a separate 2 MiB native CPU reservation. It does not release the
DDR data bridge, configure the MPFE translation unit or admit a runtime core.
No hardware operation was performed for this change.

Base: `74627be46f3c8be2d93756cfb80707f5a0768fa7`. Memory implementation:
`f5ce6650749620ce2b59e6adef9384b82a461514`. Final source, including the secure
reader guard: `26212a2703b948d40cefcd686a15444d4e12967c`.

## Resulting behavior

The source descriptor preserves the sealed package's actual on-chip or DDR
programming profile and matching control ABI. DDR selects only the proposed
bus range `0x95000000..0x95002000`; the CPU arena is
`0x95000000..0x95200000`. Its addressing label is explicitly
`physical-identity-unqualified`. CPU reservation does not prove FPGA access.

The native ELF, entry receipt and source-bound SPL/BL31 chain retain the same
memory descriptor and digest. Entry staging excludes the entire 2 MiB arena.
The real U-Boot policy rejects overlapping live LMB, stack, malloc, relocation
or FDT ranges, then reserves both the configuration arena and reader arena
before entering Go. The existing configuration arena remains
`0x94800000..0x95000000`, separate from Go RAM and the sealed FIT.

The board hook maps the reader arena as normal uncached memory using the
existing architectural page tables. A deterministic little-endian test pattern
is copied only between matching closed cold ownership observations. Successful
completion and every uncertain failure leave the reservation pinned until
reboot. Allocations returned alongside an error stay retained; no error path
calls their release callback or automatically retries.

Both native Go and the secure C reader entry reject the DDR personality's use
of the fixed on-chip recipe. The C rejection occurs before configuration
queries, register I/O or bridge changes, preserving the later read-only
identity operation. The ordinary lifecycle endpoint rejects this diagnostic
profile even when a boot-handoff verifier is requested.

## Validation

| Check | Result and scope |
| --- | --- |
| Exact affected plan | All 10 commands pass on the final source: [plan](software-plan.json), [results](software-result.json) |
| Parent checks | 1,265 tests; 2 skipped: [log](software-04.log) |
| FPGA software regression | 1,576 tests; 75 skipped. Simulation and build-tool tests only: [log](software-09.log) |
| Native lifecycle and Tenfoot | Actual prepared TF-A functions and identity/drain RTL with injected memory/bridges/SDM: [receipt](native-integration.json) |
| Secure reader boundary | 59 actual C identity/control cases; 169 secure-reader/actual Avalon RTL cases: [log](secure-reader-guard.log) |
| Reservation and ownership failures | Changed addresses, lifetime, identity, bytes, owner/generation, allocation failures and all eight uncertain firmware returns reject without release/replay: [log](focused-go-v2.log) |
| Source/receipt/staging checks | Dedicated-image, map omission/digest, complete arena exclusion and actual C live-allocation overlap checks pass: [context](python-final.log), [handoff](focused-handoff.log) |
| Actual AArch64 program cross-builds | On-chip and DDR variants compile with explicitly synthetic package metadata; no FPGA seal, usable package or hardware acceptance: [receipt](cross-fixture-result.json) |
| Actual matched boot chain | Serial/SD native entry, pinned Terasic BL31 and U-Boot compile; DDR/configuration remain disabled in this compile control: [receipt](boot-cross-result.json) |
| Generated consumers | `make check` passes: 35 consumers and 37 fixture copies; no shared definition or generated consumer changed: [log](make-check.log) |

The program/entry cross-builds ran on the memory implementation commit. The
four-file secure C guard delta changes none of their selected source inputs;
every selected hash was rechecked: [proof](reuse-source-proof.json). BL31 and
U-Boot were rebuilt on the final source. These are separate evidence scopes.

Two development failures remain retained. The first Go fixture expected the
old owner after deliberately changing the observed owner; the caller correctly
forwarded the changed scope and the fixture was repaired. The initial entry
cross-build lacked the pinned assembler's library path. The corrected run uses
the existing pinned toolchain environment; no source or check was relaxed.
Raw logs and complete private build outputs are retained separately.

## Address qualification still required

Altera's [MPFE TBU description](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/transaction-buffer-unit-interface?contentId=g1aFQI3joZYcYR61GOj4oA)
describes physical addressing through bypass and virtual addressing through
translation. The [TBU inventory](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/tbu-instances?contentId=aSdYy1HFALnhzbbzIO6M5g)
identifies the separate F2SDRAM instance. Consequently this CPU mapping cannot
stand in for a live MPFE addressing/firewall proof.

Next, qualify that path under trusted cold ownership and bind the main reader,
inherited DDR probe stop and statically held maintenance masters into the
secure master-stop callbacks. Only then prepare a newly sealed matched DDR
image and bounded TRWU8CBL read/drain/reclose trial. No new SOF/RBF or physical
DDR grant, general runtime admission or core launch/return acceptance is
claimed here. Previous on-chip hardware evidence does not qualify this change.

Raw builds, failed attempts and checked source are retained at
`/home/deano/fes-retained/de25-reader-memory-20261102/` as `source.bundle`,
`session.tar.gz`, `manifest.json` and `SHA256SUMS`. The source bundle requires
the base commit above and was independently restored in the retained verification
object store.

All files in this record are hashed in [manifest.json](manifest.json).
