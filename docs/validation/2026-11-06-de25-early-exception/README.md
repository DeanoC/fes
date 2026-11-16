# Bounded early EL3 exception capture

The previous physical trial stopped after nobits-cache-before, following
successful early checks, reset handling, CPU-data initialization and RW cache
invalidation. This change preserves the original vendor cache operations and
adds private cold exception vectors until stack selection returns. The vendor
runtime vectors are restored before BL31 setup; warm entry remains unchanged.

On an exception, the handler first installs a separate park-only vector table,
then captures ESR_EL3, ELR_EL3, FAR_EL3, SPSR_EL3 and four working registers using
registers only. One fixed-width UART record identifies the vector slot and raw
register values. Its complete message has a shared 384-byte budget, including
all strings and hexadecimal fields; every wait retains the 100,000-poll bound.
It uses no stack, function calls, candidate-memory access or RAM stores and
never resumes the interrupted operation. A logger access fault takes the
park-only vectors and cannot recursively enter the logger. The normal UART
macros default to their original behavior and preserve the early guard code.

Forty-two actual AArch64 QEMU cases cover all 16 diagnostic slots, all 16
park-only slots, real undefined-instruction/breakpoint exceptions with either
stack selection, invalid/busy UART status, recursive UART data/status faults,
and damaged first/late strings exhausting the shared 384-byte budget. Sixteen
cold/warm macro cases include injected faults before NOBITS invalidation, BSS
clear and stack selection, normal vendor-vector restoration, the real cache
helper and the original warm path. The 262 real-C-producer early ARM guard
regressions also pass. These are modeled memory/platform environments, not
physical Nano SRAM or cache qualification.

The host decoder retains at most one strict exception record and rejects boot,
even if later normal-looking markers follow. Missing fields, excess-width or
invalid values, duplicates and partial messages cannot become boot acceptance.
Seventy-six focused host tests pass. Required changed-input checks and the
matched BL31/SPL/U-Boot/file-composition checks are recorded in result.json.

The first real build was rejected because the aligned diagnostic text section
sorted ahead of the fixed BL31 entry. The correction uses the vendor .vectors
section, keeping bl31_entrypoint exactly at 0x80000000. The failed build/map and
superseded check logs are retained; the failed layout was never programmed.
A harness initially hid its expected-address symbol; another expected the
pre-stack vector slot after SPSel had changed. Both failed fixtures are retained
with the corrected ARM cases. No production fault was repaired by those harness
corrections. Initial guard-run wrappers also referenced a missing fixture copy;
the corrected real-producer runs pass.

Actual linked binary audits verify both aligned 16-slot tables, early safe VBAR
installation, canonical bounded UART accesses, register snapshots, the global
byte budget, no stack/calls/return, a permanent park, and normal vendor VBAR
restoration after stack selection and before setup. The normal early guard
remains 1,428 bytes. The private stack/BSS range, original cache operations,
ROM deadlines/frames/poll limits and ownership checks remain unchanged.

The native application and compressed entry are byte-exact retained builds from
31d716957; all 2,098 source hashes still match. The sealed FPGA payload is reused.
No new hardware evidence is claimed here. The next step is one fresh frozen
bounded trial on TRWU8CBL, preserving the factory card and other kit and stopping
at its first failed gate. A fault report is diagnostic evidence, never a grant
of boot, RAM ownership, ROM completion or core-load acceptance.
