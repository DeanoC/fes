# Matched native launcher preparation

PR #2009, runtime source `c58de370375fc5b2d85c63c763dadc267da0ea9a`.
The subsequent changes in this commit affect host preparation and observation,
not the compiled native SDK, entry or firmware source set. This is off-board
evidence. SD boot, physical keyboard launch/return and reboot persistence on
this artifact are still pending. Issue #1867 remains open for qualification.

The user explicitly selected TRWU8CBL for this native work, overriding the
general J8G routing rule: "your an exception that native tests always goto cbl,
and the spare card should be still in it". Preparation records that exception
in the continuing CBL claim. It grants no access to J8G or the factory card.

Fresh SDK, CPU entry, contained BL31, native autoboot U-Boot/SPL and complete
HPS-first SOF builds pass. The existing sealed computer and menu payloads are
reused unchanged. Complete matching checks and native file admission pass;
no existing admission is treated as live programming authority.

| Artifact | SHA-256 |
| --- | --- |
| Native FIT, 8,592,080 bytes | `add6e7391cd1724676455603f54f980f1a74dec4fc9c14d73e54cc17b316bb52` |
| Complete native SOF | `2b971e11ce1daaa4b1517adc3402d466e227524412de93bd0b3b1ff5b15ff090` |
| Regular SD image, 150,994,944 bytes | `b047317b27afdeec8a185d5acfc7f551d6e6d2debc1c88d9a504068a243986e3` |

The boot ID is `db2b79c225df0723`. The FIT retains 8,185,136 bytes of space
under its existing 16 MiB bound. The card plan is the independently recorded
spare card, with its existing recovery FIT and manifest; current physical card
identity and protected-file checks remain necessary before writes.

Regular image assembly uses the explicit return-trace recovery profile and its
pinned original producer root. The first standard-profile invocation correctly
refused this different profile. A second diagnostic exposed the need to name
the retained producer when validating the old recovery build. Both failures
are retained. The successful path uses current structural/binary checks and
the recorded producer digests without importing or executing producer code.
It rechecks recovery admission before publishing the image receipt. All eleven
launcher/FIT files, protected recovery files, raw prefix and FAT boot sectors
are read back. Fresh settings are preallocated; the regular image is not
overlaid on the kit's existing library.

Host observation now accepts explicitly selected physical launch as well as
Stop. Both use empty UART payloads and still require the complete ordered
source/owner/generation, firmware, USB and lifecycle proofs. Captures cannot
choose their own input source. A caller must arm physical input separately
and preserve the original operation deadlines from the action observation.

A fixed 64 KiB preferences upload purpose and a read-only preferences snapshot
allow a bounded operator to preserve existing settings before any proposed
seed. The snapshot checks the complete directory listing, size, ordered RAM
dump, JSON, fresh hash, scratch ownership and protected files. Missing settings
are explicit; malformed or uncertain observations retire the session. No helper
implicitly replaces preferences.

Validation: 88 joined input/uploader/preferences/paired-capture tests pass;
41 recovery/profile/image/restore tests pass; `make check` and whitespace checks
pass. The first recovery test command named a nonexistent module; its failed
wrapper is retained and the corrected command passes. The fresh nonce-bound
inspection loader and its current-source package build pass. None of these
results grants hardware acceptance or proves a reset/drain guarantee.

Working receipts are below
`out/launcher-matched-boot-20261119-r1`,
`out/launcher-recovery-gates-20261119-r1` and `out/operations` in the PR worktree.
The next hardware action is a separately frozen, read-only recovery/card and
preferences inspection, followed by a fresh bounded native trial when its
physical input and restoration prerequisites are ready.
